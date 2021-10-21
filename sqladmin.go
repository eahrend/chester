package main

import (
	"fmt"
	models "github.com/eahrend/chestermodels"
	log "github.com/sirupsen/logrus"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
	"strings"
	"time"
)

// createDatabaseReplica takes a set of configuration values and
// sends a command to the sqladmin api. On a successful call,
// it will send back a nil error code and a string that contains the
// operation ID. On an unsuccessful call, it will return a non-nil error
// and an empty string.
func createDatabaseReplica(masterInstanceName string, userLabels map[string]string, dbFlags []*sqladmin.DatabaseFlags, region, instanceName string, dataDiskSizeGb int64, tier string) (string, error) {
	var resize = true
	userLabels["chester"] = "true"
	rb := &sqladmin.DatabaseInstance{
		Name:               instanceName,
		MasterInstanceName: masterInstanceName,
		Region:             region,
		Settings: &sqladmin.Settings{
			DatabaseFlags: dbFlags,
			BackupConfiguration: &sqladmin.BackupConfiguration{
				BinaryLogEnabled: false,
				Enabled:          false,
				Kind:             "sql#backupConfiguration",
				StartTime:        "11:00",
			},
			ActivationPolicy:            "ALWAYS",
			PricingPlan:                 "PACKAGE",
			ReplicationType:             "SYNCHRONOUS",
			SettingsVersion:             1,
			DatabaseReplicationEnabled:  true,
			CrashSafeReplicationEnabled: true,
			AvailabilityType:            "ZONAL",
			Tier:                        tier,
			IpConfiguration: &sqladmin.IpConfiguration{
				Ipv4Enabled:     false,
				PrivateNetwork:  fmt.Sprintf("projects/%s/global/networks/%s", networkProjectID, networkName),
				RequireSsl:      true,
				ForceSendFields: []string{"Ipv4Enabled"},
			},
			DataDiskSizeGb:         dataDiskSizeGb,
			DataDiskType:           "PD_SSD",
			StorageAutoResize:      &resize,
			StorageAutoResizeLimit: 0,
			MaintenanceWindow: &sqladmin.MaintenanceWindow{
				Day:  0,
				Hour: 0,
				Kind: "sql#maintenanceWindow",
			},
			UserLabels: userLabels,
		},
	}
	retryCount := 0
	resp, err := sqlAdminSvc.Instances.Insert(projectID, rb).Context(ctx).Do()
	for retryCount < 120 {
		log.Debugln("Running Retry # ", retryCount)
		if err != nil {
			log.Errorf("error from inserting:", err.Error())
		}
		if resp == nil || resp.HTTPStatusCode == 409 {
			log.Debugln("Response was either 409 or nil")
			log.Debugln("HTTP Status Code:", resp.HTTPStatusCode)
			respb, _ := resp.MarshalJSON()
			log.Debugln("Response Body, if nil we'll get a panic:", string(respb))
			retryCount++
			time.Sleep(time.Second * 30)
			continue
		} else if resp.HTTPStatusCode == 200 {
			log.Debugln("Received 200 status code")
			break
		} else {
			return "", err
		}
	}

	return resp.Name, err
}

// getInstance gets a sqladmin.DatabaseInstance object based on the name of the instance.
// On a successful call, it will return a pointer to a sqladmin.DatabaseInstance and a nil error.
// On an unsuccessful call, it will return a nil object and a non-nil error.
func getInstance(name string) (*sqladmin.DatabaseInstance, error) {
	log.Debugf("Getting Instance Data \n Project ID: %s \n Instance Name: %s", projectID, name)
	resp, err := sqlAdminSvc.Instances.Get(projectID, name).Context(ctx).Do()
	if err != nil {
		log.Error(fmt.Sprintf("Failed to find instance: %s", name))
		return nil, err
	}
	return resp, err
}

// waitForOperation takes an operation ID and gets the status of it.
// This will poll until a non-nil error is returned from opSvc.Get
// or a DONE status is returned.
// If opSvc.Get returns a non-nil error, this will return a non-nil error.
func waitForOperation(opName string) error {
	opSvc := *sqladmin.NewOperationsService(sqlAdminSvc)
	for {
		result, err := opSvc.Get(projectID, opName).Do()
		if err != nil {
			return fmt.Errorf("Failed retriving operation status: %s", err)
		}
		log.Debugln("Result status:", result.Status)
		switch resultStatus := result.Status; resultStatus {
		case "DONE":
			if result.Error != nil {
				var errors []string
				for _, e := range result.Error.Errors {
					errors = append(errors, e.Message)
				}
				return fmt.Errorf(fmt.Sprintf("Operation failed with error(s): %s", strings.Join(errors, ", ")))
			}
			return nil
		default:
			resultJSON, err := result.MarshalJSON()
			if err != nil {
				return fmt.Errorf("received error from marshalling result: %s", err.Error())
			}
			log.Infoln(string(resultJSON))
		}
		time.Sleep(5 * time.Second)
	}
}

// getInstances returns a list of models.DatabaseHost structs based
// on a search and filter string.
// TODO: Remove the log.Fatal and return an error
func getInstances(search string, filter string) []models.DatabaseHost {
	dbHosts := make([]models.DatabaseHost, 0, 100)
	var (
		publicIPAddress  string
		privateIPAddress string
		req              *sqladmin.InstancesListCall
	)
	if filter != "" {
		req = sqlAdminSvc.Instances.List(projectID).Filter(filter)
	} else {
		req = sqlAdminSvc.Instances.List(projectID)
	}
	if err := req.Pages(ctx, func(page *sqladmin.InstancesListResponse) error {
		for _, databaseInstance := range page.Items {
			if search != "" {
				sc := strings.Contains(databaseInstance.Name, search)
				if sc {
					for _, ipaddress := range databaseInstance.IpAddresses {
						if ipaddress.Type == "PRIMARY" {
							publicIPAddress = ipaddress.IpAddress
						}
						if ipaddress.Type == "PRIVATE" {
							privateIPAddress = ipaddress.IpAddress
						}
					}
					dbHosts = append(dbHosts, models.DatabaseHost{databaseInstance.Name, privateIPAddress, publicIPAddress})
				}
			}
			if filter != "" {
				for _, ipaddress := range databaseInstance.IpAddresses {
					if ipaddress.Type == "PRIMARY" {
						publicIPAddress = ipaddress.IpAddress
					}
					if ipaddress.Type == "PRIVATE" {
						privateIPAddress = ipaddress.IpAddress
					}
				}
				dbHosts = append(dbHosts, models.DatabaseHost{databaseInstance.Name, privateIPAddress, publicIPAddress})
			}
		}
		return nil
	}); err != nil {
		log.Fatal(err)
	}
	return dbHosts
}

// deleteDatabaseReplica removes a read replica based on the named of the
// read replica.
// On a successful call it will return a pointer to a sqladmin.Operation struct
// and a nil error.
// On an unsuccessful call it will return a nil object and a non-nil error.
// TODO: rename function parameter 'identifier' to something like
//   instance name
func deleteDatabaseReplica(identifier string) (*sqladmin.Operation, error) {
	resp, err := sqlAdminSvc.Instances.Delete(projectID, identifier).Context(ctx).Do()
	retryCount := 0
	for retryCount < 120 {
		if err != nil && resp.HTTPStatusCode == 409 {
			log.Errorln("Received 409", err)
			retryCount++
			time.Sleep(time.Second * 30)
			continue
		} else if resp.HTTPStatusCode == 200 {
			log.Debugln("Deleted Database after retries:", retryCount)
			return resp, nil
		} else {
			log.Errorln("Unknown Error after retry:", err)
			return nil, err
		}
	}
	if err != nil {
		log.Errorln("Deleting Database Replica Error:", err)
		return nil, err
	}
	return resp, err
}
