package main

import (
	"cloud.google.com/go/datastore"
	kms "cloud.google.com/go/kms/apiv1"
	"cloud.google.com/go/pubsub"
	"context"
	"flag"
	"fmt"
	"google.golang.org/api/option"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"os"
	"path/filepath"
)

// initialize sets the configuration from the env vars
func initialize() error {
	var err error
	ctx = context.Background()
	networkProjectID = os.Getenv("NETWORK_PROJECT_ID")
	if networkProjectID == "" {
		return fmt.Errorf("failed to find network project id")
	}
	networkName = os.Getenv("NETWORK_NAME")
	if networkName == "" {
		return fmt.Errorf("failed to find network name")
	}
	projectID = os.Getenv("PROJECT_ID")
	if projectID == "" {
		return fmt.Errorf("failed to get project id")
	}
	pubSubCredFile := os.Getenv("PUBSUB_CREDS")
	if pubSubCredFile == "" {
		return fmt.Errorf("failed to get pub sub credential file")
	}
	pubsubOpts := option.WithCredentialsFile(pubSubCredFile)
	pubSubClient, err = pubsub.NewClient(ctx, projectID, pubsubOpts)
	if err != nil {
		return fmt.Errorf("failed to create new pubsub client with error: %s", err.Error())
	}
	dataStoreCredFile := os.Getenv("DATASTORE_CREDS")
	if dataStoreCredFile == "" {
		return fmt.Errorf("failed to get datastore cred file")
	}
	datastoreOpts := option.WithCredentialsFile(dataStoreCredFile)
	datastoreClient, err = datastore.NewClient(ctx, projectID, datastoreOpts)
	if err != nil {
		return fmt.Errorf("failed to create datastore client")
	}
	pst := os.Getenv("PUBSUB_TOPIC")
	if pst == "" {
		return fmt.Errorf("pubsub topic can not be empty")
	}
	topic = pubSubClient.Topic(pst)
	pss := os.Getenv("PUBSUB_SUBSCRIPTION")
	if pss == "" {
		return fmt.Errorf("pubsub subscription can not be empty")
	}
	subscription = pubSubClient.Subscription(pss)
	sqlAdminCredFile := os.Getenv("SQLADMIN_CREDS")
	if sqlAdminCredFile == "" {
		return fmt.Errorf("failed to find sql admin credential file")
	}
	opts := option.WithCredentialsFile(sqlAdminCredFile)
	sqlAdminSvc, err = sqladmin.NewService(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to create sqladminsvc with error: %s", err.Error())
	}
	kmsCredFile := os.Getenv("KMS_CREDS")
	kmsOpts := option.WithCredentialsFile(kmsCredFile)
	kmsClient, err = kms.NewKeyManagementClient(ctx, kmsOpts)
	if err != nil {
		return fmt.Errorf("failed to create key management client with error :%s", err.Error())
	}
	var config *rest.Config
	if os.Getenv("IN_CLUSTER") == "true" {
		config, err = rest.InClusterConfig()
		if err != nil {
			return fmt.Errorf("failed to create in cluster configuration :%s", err.Error())
		}
	} else {
		var kubeconfig *string
		if home := homedir.HomeDir(); home != "" {
			kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
		} else {
			kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
		}
		flag.Parse()

		// use the current context in kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
		if err != nil {
			return fmt.Errorf("failed creating out of cluster configuration: %s", err.Error())
		}
	}

	kubeClient, err = kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create new kuberenetes client from config: %s", err.Error())
	}

	return nil
}
