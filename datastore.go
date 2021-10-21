package main

// TODO: Can probably combine a lot of these into one function
import (
	"cloud.google.com/go/datastore"
	models "github.com/eahrend/chestermodels"
	log "github.com/sirupsen/logrus"
	it "google.golang.org/api/iterator"
)

// getIncidentState gets the current state of the incident from datastore
func getIncidentState(id string) (string, error) {
	e := datastore.NewQuery("incident").Namespace("chester").Filter("IncidentID=", id)
	iter := datastoreClient.Run(ctx, e)
	for {
		incident := &models.DataStoreIncident{}
		incidentKey, err := iter.Next(incident)
		switch err {
		case nil:
			err = datastoreClient.Get(ctx, incidentKey, incident)
			if err != nil {
				return "", err
			}
			return incident.State, nil
		case it.Done:
			log.Infoln("No more things to iterate over")
			return "", nil
		default:
			log.WithField("event", "retrieving the incident to remove").Error(err)
			return "", nil
		}
	}
}

// updateOperationID updates the current operation id from sqladminsvc
// this is used in case the app goes down during a sqlupdate command
func updateOperationID(id, operation string) error {
	_, err := datastoreClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		dsi := &models.DataStoreIncident{}
		log.Debugf("adding operation ID %s to incident %s ", operation, id)
		key := datastore.NameKey("incident", id, nil)
		key.Namespace = "chester"
		if err := tx.Get(key, dsi); err != nil {
			log.Debugln("Error getting instance group key", err.Error())
			return err
		}
		dsi.OperationID = operation
		if _, err := tx.Put(key, dsi); err != nil {
			log.Debugln("Error putting instance group key", err.Error())
			return err
		}
		return nil
	})
	return err
}

// updateLastIPAddress updates the generated read replica
// ip address
func updateLastIPAddress(id, ipAddress string) error {
	_, err := datastoreClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		dsi := &models.DataStoreIncident{}
		log.Debugf("adding ip address %s to incident %s", ipAddress, id)
		key := datastore.NameKey("incident", id, nil)
		key.Namespace = "chester"
		if err := tx.Get(key, dsi); err != nil {
			log.Debugln("Error getting instance group key", err.Error())
			return err
		}
		dsi.LastIPAddress = ipAddress
		if _, err := tx.Put(key, dsi); err != nil {
			log.Debugln("Error putting instance group key", err.Error())
			return err
		}
		return nil
	})
	return err
}

// addReplicaToDatastore adds an IP address to the datastore config
// this is done by adding a ProxySqlMySqlServer struct to the list of
// read replicas
func addReplicaToDatastore(instanceGroup, ipAddress string) error {
	_, err := datastoreClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		psqlconfig, err := getProxySQLConfig(instanceGroup)
		if err != nil {
			return err
		}
		// TODO: Once proxysql adds instance:ssl config we can add the proxysql config stuff here
		newMySqlServer := models.ProxySqlMySqlServer{
			Address:   ipAddress,
			Port:      3306,
			Hostgroup: psqlconfig.ReadHostGroup,
			// TODO: add dynamic max conns
			MaxConnections: 100,
			Comment:        models.AddedByChester,
			UseSSL:         psqlconfig.UseSSL,
		}
		psqlconfig.AddReadReplica(newMySqlServer)
		key := datastore.NameKey("proxysqlconfig", instanceGroup, nil)
		key.Namespace = "chester"
		// next part will be breaking this into two separate functions
		_, err = tx.Put(key, psqlconfig)
		if err != nil {
			tx.Rollback()
			return err
		}
		return nil
	})
	return err
}

// updateLastReadReplica adds the last read replica added from the incident.
// This is used in case there is a shutdown of the application
func updateLastReadReplica(id, replicaName string) error {
	_, err := datastoreClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		dsi := &models.DataStoreIncident{}
		log.Debugf("adding last read replica %s to incident %s: ", replicaName, id)
		key := datastore.NameKey("incident", id, nil)
		key.Namespace = "chester"
		if err := tx.Get(key, dsi); err != nil {
			log.Debugln("Error getting instance group key", err.Error())
			return err
		}
		dsi.LastReadReplicaName = replicaName
		if _, err := tx.Put(key, dsi); err != nil {
			log.Debugln("Error putting instance group key", err.Error())
			return err
		}
		return nil
	})
	return err
}

// updateLastProcess stores the last process in datastore, in case of a restart
// we aren't replicating work.
func updateLastProcess(id, process string) error {
	_, err := datastoreClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		dsi := &models.DataStoreIncident{}
		log.Debugf("Updating last process %s for incident %s: ", process, id)
		key := datastore.NameKey("incident", id, nil)
		key.Namespace = "chester"
		if err := tx.Get(key, dsi); err != nil {
			log.Debugln("Error getting instance group key", err.Error())
			return err
		}
		dsi.LastProcess = process
		dsi.LastUpdatedBy = "daemon"
		if _, err := tx.Put(key, dsi); err != nil {
			log.Debugln("Error putting instance group key", err.Error())
			return err
		}
		return nil
	})
	return err
}

// getProxySQLConfig returns the proxysql config in the form of a ProxySqlConfig
// struct, which can than be turned into a JSON object or a libconfig object
func getProxySQLConfig(instanceGroup string) (*models.ProxySqlConfig, error) {
	psqlConfig := models.NewProxySqlConfig()
	_, err := datastoreClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		log.Debugf("getting proxysql config for instance group: %s", instanceGroup)
		key := datastore.NameKey("proxysqlconfig", instanceGroup, nil)
		key.Namespace = "chester"
		if err := tx.Get(key, psqlConfig); err != nil {
			log.Debugln("Error getting instance group key", err.Error())
			return err
		}
		return nil
	})
	return psqlConfig, err
}

// delete incident removes the incident from datastore, this should only be called
// during the closure of an incident
func deleteIncident(incidentID string) (string, error) {
	e := datastore.NewQuery("incident").Namespace("chester").Filter("IncidentID=", incidentID)
	iter := datastoreClient.Run(ctx, e)
	for {
		incident := &models.DataStoreIncident{}
		incidentKey, err := iter.Next(incident)
		switch err {
		case nil:
			err = datastoreClient.Delete(ctx, incidentKey)
			if err != nil {
				log.Errorln(err)
			}
			return "", err
		case it.Done:
			log.Debugln("No more things to iterate over")
			return "", nil
		default:
			log.WithField("event", "retrieving the incident to remove").Error(err)
			return "", nil
		}
	}
}

// removeReplicaFromDataStoreConfigMap removes an instance from the proxysql
// config in datastore. The name is a little misleading, since it doesn't
// actually update the configmap in k8s.
func removeReplicaFromDataStoreConfigMap(instanceGroup, ipaddress string) error {
	_, err := datastoreClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		psqlconfig, err := getProxySQLConfig(instanceGroup)
		if err != nil {
			return err
		}
		sqlServers := psqlconfig.MySqlServers
		newSqlServers := []models.ProxySqlMySqlServer{}
		for _, server := range sqlServers {
			if server.Address != ipaddress {
				newSqlServers = append(newSqlServers, server)
			}
		}
		psqlconfig.MySqlServers = newSqlServers
		key := datastore.NameKey("proxysqlconfig", instanceGroup, nil)
		key.Namespace = "chester"
		_, err = tx.Put(key, psqlconfig)
		if err != nil {
			tx.Rollback()
			return err
		}
		return nil
	})
	return err
}

// getDataStoreIncidents gets a list of both active and closed incidents in datastore.
func getDataStoreIncidents(iterator *datastore.Iterator) ([]models.DataStoreIncident, error) {
	var incidentSlice []models.DataStoreIncident
	for {
		incident := models.DataStoreIncident{}
		_, err := iterator.Next(&incident)
		switch err {
		case nil:
			incidentSlice = append(incidentSlice, incident)
		case it.Done:
			return incidentSlice, nil
		default:
			log.WithField("event", "iterating over incidents").Error(err)
			return nil, err
		}
	}
}

// getChesterMetaData gets the ChesterMetaData struct from datastore.
func getChesterMetaData(instanceGroup string) (models.ChesterMetaData, error) {
	chesterMetaData := models.ChesterMetaData{}
	_, err := datastoreClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		parent := generateChesterKey(instanceGroup)
		key := generateMetaDataKey(parent)
		if err := tx.Get(key, &chesterMetaData); err != nil {
			return err
		}
		return nil
	})
	return chesterMetaData, err
}

// generateChesterKey generates a proxysql config key in the
// chester namespace with the instancegroup as the ID
// TODO: rename this to better reflect what it does
func generateChesterKey(instanceGroup string) *datastore.Key {
	key := datastore.NameKey("proxysqlconfig", instanceGroup, nil)
	key.Namespace = "chester"
	return key
}

// generateMetaDataKey creates a key with a relation to a parent key
func generateMetaDataKey(parent *datastore.Key) *datastore.Key {
	key := datastore.NameKey(models.MetaData, parent.Name, parent)
	key.Namespace = "chester"
	return key
}
