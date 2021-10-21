package main

import (
	"encoding/json"
	"time"

	"cloud.google.com/go/datastore"
	"cloud.google.com/go/pubsub"
	models "github.com/eahrend/chestermodels"
	log "github.com/sirupsen/logrus"
)

// startup gets a list of active and closed incidents
// as well as older incidents that may have not been resolved.
func startup() error {
	closedQuery := datastore.NewQuery("incident").Namespace("chester").Filter("State=", "closed")
	closedIterator := datastoreClient.Run(ctx, closedQuery)
	closedIncidents, err := getDataStoreIncidents(closedIterator)
	if err != nil {
		log.Errorln(err)
		return err
	}
	openQuery := datastore.NewQuery("incident").Namespace("chester").Filter("State=", "open")
	openIterator := datastoreClient.Run(ctx, openQuery)
	openIncidents, err := getDataStoreIncidents(openIterator)
	if err != nil {
		log.Errorln(err)
		return err
	}
	incidentsToRun, incidentsToClose := getExpiredIncidents(openIncidents, closedIncidents)
	log.Debugln("Number of incidents to rerun:", len(incidentsToRun))
	log.Debugln("Number of incidents to close:", len(incidentsToClose))
	for _, incident := range incidentsToClose {
		_, err = deleteIncident(incident.IncidentID)
		if err != nil {
			log.Errorf("failed to delete incident %s from startup script %s", incident.IncidentID, err)
			return err
		}
	}
	for _, incident := range incidentsToRun {
		b, err := json.Marshal(&incident)
		if err != nil {
			log.Errorf("failed to marshal incident %s from startup script %s", incident.IncidentID, err)
			return err
		}
		topic.Publish(ctx, &pubsub.Message{
			Data: b,
		})
	}
	return nil
}

// getExpiredIncidents combines the explicitly closed incidents with the expired incidents
func getExpiredIncidents(incidents []models.DataStoreIncident, closedIncidents []models.DataStoreIncident) ([]models.DataStoreIncident, []models.DataStoreIncident) {
	for k, incident := range incidents {
		openTime := time.Unix(incident.StartedAt, 0)
		nowTime := time.Now()
		diff := nowTime.Sub(openTime)
		if diff > 3*time.Hour {
			closedIncidents = append(closedIncidents, incident)
			incidents = append(incidents[:k], incidents[k+1:]...)
		}
	}
	return incidents, closedIncidents
}
