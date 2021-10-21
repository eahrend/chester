package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"cloud.google.com/go/pubsub"
	models "github.com/eahrend/chestermodels"
	log "github.com/sirupsen/logrus"
)

// run is the main runner function, handles retreiving events from pub/sub
func run() error {
	funclog := log.WithFields(log.Fields{
		"func": "run",
	})
	funclog.Debugln("Checking datastore for config")
	funclog.Debugln("Starting message Subscription polling")
	err := subscription.Receive(context.Background(), func(ctx context.Context, m *pubsub.Message) {
		funclog.Debugln("!!!MESSAGE RECEIVED!!!")
		m.Ack()
		handleEvent(m)
	})
	funclog.Errorln("Received error from subscription receive: %s", err.Error())
	return err
}

// handleEvent processes message from pubsub and determines
// what to do with it
func handleEvent(message *pubsub.Message) {
	funclog := log.WithFields(log.Fields{
		"func": "handleEvent",
	})
	var m models.DataStoreIncident
	messageBytes := message.Data
	dec := json.NewDecoder(strings.NewReader(string(messageBytes)))
	if err := dec.Decode(&m); err == io.EOF {
		funclog.Fatal("Failed to decode the message")
	} else if err != nil {
		funclog.Fatal("Failed to Decode message: ", err)
	}
	switch action := m.Action; action {
	case "add":
		funclog.Debugln("Add Action")
		status, err := addReplica(m)
		if err != nil {
			log.Errorf("failed to add replica with error: %s on process: %s", err.Error(), status)
		}
		funclog.Debugf("Finished with status of %s", status)
	case "remove":
		funclog.Debugln("Remove Action")
		status, err := removeReplica(m)
		if err != nil {
			funclog.Errorf("failed to remove replica with error: %s on process: %s", err.Error(), status)
		}
		funclog.Debugf("Finished with status of %s", status)
	case "restart":
		funclog.Debugln("Restart proxysql")
		status, err := restartProxySQL(m)
		if err != nil {
			funclog.Errorf("failed to restart proxysql with error: %s on process: %s", err.Error(), status)
		}
		funclog.Debugf("Finished with status of %s", status)
	default:
		funclog.Debugf("Failed to find proper action.\n Message %v \n Action: %s", m, action)
	}
}

// addReplica adds a new read replica to the list of readers.
// This is handled recursively, in case the pod needs to restart
func addReplica(incident models.DataStoreIncident) (string, error) {
	funclog := log.WithFields(log.Fields{
		"func":     "addReplica",
		"incident": incident.IncidentID,
	})
	if incident.State == models.Closed {
		funclog.Debugf("received closed state from GCF")
		return "", nil
	}
	switch lastProcess := incident.LastProcess; lastProcess {
	case models.GCFPush:
		sendMessages([]byte(fmt.Sprintf("Received a scale up message \n IncidentID: %s \n Database: %s \n Project: %s", incident.IncidentID, incident.SqlMasterInstance, projectID)))
		err := updateLastProcess(incident.IncidentID, models.DaemonAck)
		if err != nil {
			funclog.WithField("lastProcess", models.GCFPush).Errorf("failed to update last process with error %s", err.Error())
			return models.Fail, err
		}
		incident.LastProcess = models.DaemonAck
		return addReplica(incident)
	case models.DaemonAck:
		instances := getInstances("", "settings.userLabels.chester:true")
		chesterMetaData, err := getChesterMetaData(incident.SqlMasterInstance)
		if err != nil {
			funclog.WithField("lastProcess", models.DaemonAck).Errorf("failed to get chester metadata with error %s", err.Error())
			return "fail", err
		}
		if len(instances) >= chesterMetaData.MaxChesterInstances {
			funclog.Warnf("max instances reached")
			sendMessages([]byte(fmt.Sprintf("Too many instances, need to modify the scaling threshold, JIRA ticket soon to come \n IncidentID: %s \n Database: %s \n ProjectID: %s", incident.IncidentID, incident.SqlMasterInstance, projectID)))
			return "fail", fmt.Errorf("max instances reached")
		}
		instanceName := generateInstanceName(incident)
		funclog.Debugln("Creating Database replica with name of:", instanceName)
		sendMessages([]byte(fmt.Sprintf("Creating new database replica with name: %s \n IncidentID: %s \n Database: %s \n Project: %s", instanceName, incident.IncidentID, incident.SqlMasterInstance, projectID)))
		masterData, err := getInstance(incident.SqlMasterInstance)
		if err != nil {
			funclog.WithField("lastProcess", models.DaemonAck).Errorf("failed to get instance with error %s", err.Error())
			return models.Fail, err
		}
		operationID, err := createDatabaseReplica(incident.SqlMasterInstance, masterData.Settings.UserLabels, masterData.Settings.DatabaseFlags, masterData.Region, instanceName, masterData.Settings.DataDiskSizeGb, masterData.Settings.Tier)
		if err != nil {
			funclog.WithField("lastProcess", models.DaemonAck).Errorf("failed to get operationID with error %s", err.Error())
			return models.Fail, err
		}
		incident.LastProcess = models.InstanceInsert
		incident.OperationID = operationID
		incident.LastReadReplicaName = instanceName
		err = updateLastProcess(incident.IncidentID, models.InstanceInsert)
		if err != nil {
			funclog.WithField("lastProcess", models.DaemonAck).Errorf("failed to UpdateLastProcess with error %s", err.Error())
			return models.Fail, err
		}
		err = updateLastReadReplica(incident.IncidentID, instanceName)
		if err != nil {
			funclog.WithField("lastProcess", models.DaemonAck).Errorf("failed to UpdateLastReadReplica with error %s", err.Error())
			return models.Fail, err
		}
		err = updateOperationID(incident.IncidentID, operationID)
		if err != nil {
			funclog.WithField("lastProcess", models.DaemonAck).Errorf("failed to UpdateOperationID with error %s", err.Error())
			return models.Fail, err
		}
		return addReplica(incident)
	case models.InstanceInsert:
		sendMessages([]byte(fmt.Sprintf("Waiting on operation: %s \n IncidentID: %s \n Database: %s \n Project: %s", incident.OperationID, incident.IncidentID, incident.SqlMasterInstance, projectID)))
		err := waitForOperation(incident.OperationID)
		if err != nil {
			sendMessages([]byte(fmt.Sprintf("Failed for wait operation: %s \n IncidentID: %s \n Database: %s \n Project: %s", err.Error(), incident.IncidentID, incident.SqlMasterInstance, projectID)))
			funclog.WithField("lastProcess", models.InstanceInsert).Errorf("failed to waitForOperation with error %s", err.Error())
			return models.Fail, err
		}
		sendMessages([]byte(fmt.Sprintf("Operation succeeded \n IncidentID: %s \n Database: %s \n Project: %s", incident.IncidentID, incident.SqlMasterInstance, projectID)))
		dbs, err := getInstance(incident.LastReadReplicaName)
		if err != nil {
			funclog.WithField("lastProcess", models.InstanceInsert).Errorf("failed to getInstance with error %s", err.Error())
			return models.Fail, err
		}
		ipAddress := getPrivateIP(dbs.IpAddresses)
		incident.LastIPAddress = ipAddress
		err = updateLastIPAddress(incident.IncidentID, ipAddress)
		if err != nil {
			funclog.WithField("lastProcess", models.InstanceInsert).Errorf("failed to UpdateLastIPAddress with error %s", err.Error())
			return models.Fail, err
		}
		err = addReplicaToDatastore(incident.SqlMasterInstance, ipAddress)
		if err != nil {
			funclog.WithField("lastProcess", models.InstanceInsert).Errorf("failed to UpdateLastIPAddress with error %s", err.Error())
			return models.Fail, err
		}
		incident.LastProcess = models.ConfigUpdate
		err = updateLastProcess(incident.IncidentID, models.ConfigUpdate)
		if err != nil {
			funclog.WithField("lastProcess", models.InstanceInsert).Errorf("failed to UpdateLastProcess with error %s", err.Error())
			return models.Fail, err
		}
		return addReplica(incident)
	case models.ConfigUpdate:
		sendMessages([]byte(fmt.Sprintf("Updating k8s config \n IncidentID: %s \n Database: %s \n Project: %s", incident.IncidentID, incident.SqlMasterInstance, projectID)))
		err := updateConfigMap(incident.SqlMasterInstance)
		if err != nil {
			funclog.WithField("lastProcess", models.ConfigUpdate).Errorf("failed to updateConfigMap with error %s", err.Error())
			return models.Fail, err
		}
		incident.LastProcess = models.ProxysqlRestart
		err = updateLastProcess(incident.IncidentID, models.ProxysqlRestart)
		if err != nil {
			funclog.WithField("lastProcess", models.ConfigUpdate).Errorf("failed to UpdateLastProcess with error %s", err.Error())
			return models.Fail, err
		}
		return addReplica(incident)
	case models.ProxysqlRestart:
		sendMessages([]byte(fmt.Sprintf("Rolling restart of proxysql instances \n IncidentID: %s \n Database: %s \n Project: %s", incident.IncidentID, incident.SqlMasterInstance, projectID)))
		err := reloadProxySql(incident.SqlMasterInstance)
		if err != nil {
			funclog.WithField("lastProcess", models.ProxysqlRestart).Errorf("failed to reloadProxySql with error %s", err.Error())
			return models.Fail, err
		}
		incident.LastProcess = models.StatusCheck
		err = updateLastProcess(incident.IncidentID, models.StatusCheck)
		if err != nil {
			funclog.WithField("lastProcess", models.ProxysqlRestart).Errorf("failed to UpdateLastProcess with error %s", err.Error())
			return models.Fail, err
		}
		return addReplica(incident)
	case models.StatusCheck:
		sendMessages([]byte(fmt.Sprintf("Starting cooldown period \n IncidentID: %s \n Database: %s \n Project: %s", incident.IncidentID, incident.SqlMasterInstance, projectID)))
		funclog.Debugf("Starting cooldown timer for incident %s", incident.IncidentID)
		status, err := coolDownTimer(incident)
		funclog.Debugf("received return status from cooldown timer: %s", status)
		if err != nil {
			funclog.Errorf("received error from cooldown timer: %s", err.Error())
			return models.Fail, err
		}
		if status != models.Closed {
			sendMessages([]byte(fmt.Sprintf("Status not closed adding another replica \n IncidentID: %s \n Database: %s \n Project: %s", incident.IncidentID, incident.SqlMasterInstance, projectID)))
			funclog.Debugf("status not closed, adding another replica")
			incident.LastProcess = models.DaemonAck
			err = updateLastProcess(incident.IncidentID, models.DaemonAck)
			if err != nil {
				funclog.WithField("lastProcess", models.StatusCheck).Errorf("failed to UpdateLastProcess with error %s", err.Error())
				return models.Fail, err
			}
			return addReplica(incident)
		} else {
			sendMessages([]byte(fmt.Sprintf("Incident closed \n IncidentID: %s \n Database: %s \n Project: %s", incident.IncidentID, incident.SqlMasterInstance, projectID)))
			funclog.Debugf("status is listed as closed, ending loop")
			incident.LastProcess = models.Closed
			err = updateLastProcess(incident.IncidentID, "closed")
			if err != nil {
				funclog.WithField("lastProcess", models.StatusCheck).Errorf("failed to UpdateLastProcess with error %s", err.Error())
				return models.Fail, err
			}
			return addReplica(incident)
		}
	case models.Closed:
		// delete incident id
		sendMessages([]byte(fmt.Sprintf("Incident closed \n IncidentID: %s \n Database: %s \n Project: %s", incident.IncidentID, incident.SqlMasterInstance, projectID)))
		err := updateLastProcess(incident.IncidentID, models.Clear)
		if err != nil {
			funclog.WithField("lastProcess", models.Closed).Errorf("failed to UpdateLastProcess with error %s", err.Error())
			return models.Fail, err
		}
		incident.LastProcess = models.Clear
		_, err = deleteIncident(incident.IncidentID)
		if err != nil {
			funclog.WithField("lastProcess", models.Closed).Errorf("failed to DeleteIncident with error %s", err.Error())
			return models.Fail, err
		} else {
			return addReplica(incident)
		}
	default:
		var err error
		if lastProcess != models.Clear {
			err = errors.New(fmt.Sprintf("unknown status %s", lastProcess))
		}
		return lastProcess, err
	}
}

// removeReplica removes a chester created replica from
func removeReplica(incident models.DataStoreIncident) (string, error) {
	funclog := log.WithFields(log.Fields{
		"func":     "removeReplica",
		"incident": incident.IncidentID,
	})
	if incident.State == models.Closed {
		funclog.Debugf("received closed state from GCF")
		return "", nil
	}
	switch lastProcess := incident.LastProcess; lastProcess {
	case models.GCFPush:
		sendMessages([]byte(fmt.Sprintf("Received scale down alert \n IncidentID: %s \n Database: %s \n Project: %s", incident.IncidentID, incident.SqlMasterInstance, projectID)))
		err := updateLastProcess(incident.IncidentID, models.DaemonAck)
		if err != nil {
			return models.Fail, err
		}
		incident.LastProcess = models.DaemonAck
		return removeReplica(incident)
	case models.DaemonAck:
		// get a list of the instances and make the call to remove one
		// TODO: Probably need to alert on this issue
		instances := getInstances("", "settings.userLabels.chester:true")
		if len(instances) == 0 {
			sendMessages([]byte(fmt.Sprintf("Scale down failed, threshold too low, JIRA ticket to come \n IncidentID: %s \n Database: %s \n Project: %s", incident.IncidentID, incident.SqlMasterInstance, projectID)))
			return models.Closed, nil
		}
		sendMessages([]byte(fmt.Sprintf("Removing instance: %s \n IncidentID: %s \n Database: %s \n Project: %s", instances[0].Name, incident.IncidentID, incident.SqlMasterInstance, projectID)))
		h, err := getInstance(instances[0].Name)
		if err != nil {
			funclog.Errorf("failed to find instance: %s", err.Error())
			return models.Fail, err
		}
		ip := getPrivateIP(h.IpAddresses)
		err = updateLastIPAddress(incident.IncidentID, ip)
		if err != nil {
			funclog.Errorf("failed to update last ip address: %s", err.Error())
			return models.Fail, err
		}
		incident.LastReadReplicaName = h.Name
		err = updateLastReadReplica(incident.IncidentID, h.Name)
		if err != nil {
			funclog.Errorf("failed to update last ip address: %s", err.Error())
			return models.Fail, err
		}
		err = removeReplicaFromDataStoreConfigMap(incident.SqlMasterInstance, ip)
		if err != nil {
			funclog.Errorf("failed to update last ip address: %s", err.Error())
			return models.Fail, err
		}
		incident.LastProcess = models.ConfigUpdate
		err = updateLastProcess(incident.IncidentID, models.ConfigUpdate)
		return removeReplica(incident)
	case models.ConfigUpdate:
		err := updateConfigMap(incident.SqlMasterInstance)
		if err != nil {
			return models.Fail, err
		}
		incident.LastProcess = models.ProxysqlRestart
		err = updateLastProcess(incident.IncidentID, models.ProxysqlRestart)
		return removeReplica(incident)
	case models.ProxysqlRestart:
		sendMessages([]byte(fmt.Sprintf("Rolling restart of proxysql \n IncidentID: %s \n Database: %s \n Project: %s", incident.IncidentID, incident.SqlMasterInstance, projectID)))
		err := reloadProxySql(incident.SqlMasterInstance)
		if err != nil {
			return models.Fail, err
		}
		incident.LastProcess = models.InstanceInsert
		err = updateLastProcess(incident.IncidentID, models.InstanceInsert)
		return removeReplica(incident)
	case models.InstanceInsert:
		resp, err := deleteDatabaseReplica(incident.LastReadReplicaName)
		if err != nil {
			return models.Fail, err
		}
		incident.OperationID = resp.Name
		sendMessages([]byte(fmt.Sprintf("Destroying instance operation ID: %s \n IncidentID: %s \n Database: %s \n Project: %s", incident.OperationID, incident.IncidentID, incident.SqlMasterInstance, projectID)))
		err = updateOperationID(incident.IncidentID, resp.Name)
		if err != nil {
			return models.Fail, err
		}
		incident.LastProcess = models.StatusCheck
		err = updateLastProcess(incident.IncidentID, models.StatusCheck)
		if err != nil {
			return models.Fail, err
		}
		return removeReplica(incident)
	case models.StatusCheck:
		funclog.Debugf("status check on waiting for operation")
		sendMessages([]byte(fmt.Sprintf("Waiting for operation: %s \n IncidentID: %s \n Database: %s \n Project: %s", incident.OperationID, incident.IncidentID, incident.SqlMasterInstance, projectID)))
		err := waitForOperation(incident.OperationID)
		if err != nil {
			sendMessages([]byte(fmt.Sprintf("Operation Failed \n error: %s \n Operation ID: %s \n IncidentID: %s \n Database: %s \n Project: %s", err.Error(), incident.OperationID, incident.IncidentID, incident.SqlMasterInstance, projectID)))
			return models.Fail, err
		}
		funclog.Debugf("cooldown for removal timer started")
		status, err := coolDownTimer(incident)
		if err != nil {
			return models.Fail, err
		}
		if status != models.Closed {
			sendMessages([]byte(fmt.Sprintf("Cooldown period passed, removing another instance \n IncidentID: %s \n Database: %s \n Project: %s", incident.IncidentID, incident.SqlMasterInstance, projectID)))
			funclog.Debugf("still need to remove replicas")
			err = updateLastProcess(incident.IncidentID, models.DaemonAck)
			incident.LastProcess = models.DaemonAck
			if err != nil {
				return models.Fail, err
			}
			return removeReplica(incident)
		} else {
			sendMessages([]byte(fmt.Sprintf("Removal Incident closed \n IncidentID: %s \n Database: %s \n Project: %s", incident.IncidentID, incident.SqlMasterInstance, projectID)))
			incident.LastProcess = "closed"
			err = updateLastProcess(incident.IncidentID, "closed")
			if err != nil {
				return models.Fail, err
			}
			return removeReplica(incident)
		}
	case models.Closed:
		err := updateLastProcess(incident.IncidentID, "closed")
		if err != nil {
			return models.Fail, err
		}
		incident.LastProcess = models.Clear
		_, err = deleteIncident(incident.IncidentID)
		if err != nil {
			return models.Fail, err
		}
		return removeReplica(incident)
	default:
		var err error
		if lastProcess != models.Clear {
			err = errors.New(fmt.Sprintf("unknown status %s", lastProcess))
		}
		return lastProcess, err
	}
}

// this doesn't require the update and sturdiness, as of yet, cause these aren't created in datastore
func restartProxySQL(incident models.DataStoreIncident) (string, error) {
	err := updateConfigMap(incident.SqlMasterInstance)
	if err != nil {
		return models.Fail, err
	}
	err = reloadProxySql(incident.SqlMasterInstance)
	if err != nil {
		return models.Fail, err
	}
	return "", nil
}
