package main

import (
	"cloud.google.com/go/datastore"
	kms "cloud.google.com/go/kms/apiv1"
	"cloud.google.com/go/pubsub"
	"context"
	log "github.com/sirupsen/logrus"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
	"k8s.io/client-go/kubernetes"
)

// networkProjectID is the name of the shared vpc project ID
var networkProjectID string

// networkName is the name of the shared vpc network
var networkName string

// projectID refers to the GCP project id that this instance of chester resides in
var projectID string

// kubeClient is the kubernetes client used between different functions in the runtime
var kubeClient *kubernetes.Clientset

// sqlAdminSvc is the sql admin api client used between different functions in the runtime
var sqlAdminSvc *sqladmin.Service

// datastoreClient is the datastore client used between different functions in the runtime
var datastoreClient *datastore.Client

// kmsClient is the client that allows the daemon to interact with the key management API
var kmsClient *kms.KeyManagementClient

// ctx is a common context shared between functions
var ctx context.Context

// pubSubClient is the pub/sub client used between different functions in the runtime
var pubSubClient *pubsub.Client

// topic is the name of the pubsub topic used to broadcast messages to chester clients
var topic *pubsub.Topic

// subscription is the name of the pubsub subscription that we'll listen to
var subscription *pubsub.Subscription

// entrypoint, duh.
func main() {
	// TODO: set this to be configurable
	log.SetLevel(log.DebugLevel)
	err := initialize()
	if err != nil {
		log.Fatalf("failed during initialization: %s", err.Error())
	}
	// do some initial scanning to ensure we're not stepping on toes
	err = startup()
	if err != nil {
		log.Fatalf("failed during initial datastore sweep: %s", err.Error())
	}
	// run starts the actual application
	err = run()
	log.Fatalf("error from runner: %s", err.Error())
}
