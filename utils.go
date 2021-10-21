package main

import (
	"fmt"
	"time"

	models "github.com/eahrend/chestermodels"
	"github.com/google/uuid"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
)

// generateInstanceName creates a random instance name.
func generateInstanceName(incident models.DataStoreIncident) string {
	endHash, _ := uuid.NewRandom()
	instanceName := fmt.Sprintf("%s%s", incident.ReplicaBaseName, endHash)
	return instanceName
}

// getPrivateIP is a helper function which returns the private IP address from
// a list of sqladmin.IpMappings
func getPrivateIP(IPAddesses []*sqladmin.IpMapping) string {
	for _, ipaddress := range IPAddesses {
		if ipaddress.Type == "PRIVATE" {
			return ipaddress.IpAddress
		}
	}
	return ""
}

// coolDownTimer waits 5 minutes between adding/removing a replica
// and adding/removing a new replica, to prevent us from scaling too fast.
// TODO: make the timer configurable
func coolDownTimer(incident models.DataStoreIncident) (string, error) {
	time.Sleep(5 * time.Minute)
	incidentState, err := getIncidentState(incident.IncidentID)
	if err != nil {
		return models.Fail, err
	}
	if incidentState != models.Closed {
		return models.StatusCheck, err
	} else {
		return models.Closed, nil
	}
}
