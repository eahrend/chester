# Chester


Chester is an autoscaling tool for CloudSQL. This leverages Stackdriver Monitoring, pubsub and cloud functions.
It has multiple moving pieces that have their own sections in this doc.
* Chester-Daemon
  * Deployment on GKE that listens to pubsub and has administrative access to modify configmaps and other namespaced deployments in the cluster
  * Scales up/down read replicas in cloudsql
  * Updates the configmap on ProxySQL to enable reading/writing to those hostgroups
  * Repository - This one
* ChesterModels
  * Helper functions and common structs across the ecosystem
* GCF
  * Takes events from stackdriver and converts them into tables in datastore and messages in pub/sub
  * Repository - https://github.com/eahrend/chester-gcf
* ProxySQL
  * Exists as a deployment in GKE, currently non-clusterized, so the configuration is stored in a configmap
  * Applications get in contact with proxysql via the internal service endpoint in K8S
* Chester-API
  * HTTP API for clients (i.e terraform) to allow for programatic configuration.
  * Repository - https://github.com/eahrend/chester-api
* Datastore
  * Persistent no-sql storage for event data
* Pub/Sub
  * Async messaging for alerting.

## Why
Because Cloud SQL is missing the functionality that AWS Aurora has in Google Cloud. The official word from Google Cloud SQL teams word is leverage cloud functions to do this work, however their official implementation leaves a lot to be desired, so this is the expansion.

## How it works at a high level
This works by setting up an alert in stackdriver with instance metadata in the details, scaling up/down, then updating the configuration

## Configs
* NETWORK_PROJECT_ID - The project ID of the shared vpc host project.
* NETWORK_NAME - The name of the shared vpc host network.
* PROJECT_ID - Project ID of the GCP project where chester exists.
* PUBSUB_CREDS - Physical location of the JSON token we use to auth against pubsub.
* DATASTORE_CREDS - Physical location of the JSON token we use to auth against datastore.
* PUBSUB_TOPIC - Name of the topic used to broadcast messages to chester services
* PUBSUB_SUBSCRIPTION - Name of the subscription used to listen to messages from the topic
* SQLADMIN_CREDS - Physical location of the JSON token we use to auth against the sqladmin api.
* IN_CLUSTER - Boolean, whether or not the daemon is in the cluster or not, used primarily for dev work when you don't want to spin up minikube
 

## Stackdriver
### Alerting Policy
`Violates when: Any cloudsql.googleapis.com/database/network/connections stream is <above/below> a threshold of n for greater than <1/5> minute(s)`

Documentation in these alerts needs to have instance/policy metadata in documentation.

Example:
```json
{ 
  "replica_basename":"sql-development-", 
  "sql_master_instance":"sql-development", 
  "action":"add" 
}
```
Parameters:
* replica_basename = string, what the default basename for the read replicas are
* sql_master_instance = string, name of the immutable writer
* action = string, are we adding or removing a read replica


## GCF
The google cloud function takes the event data from stackdriver, converts it into a datastore object in the chester namespace, then sends that message to pub/sub

## Datastore
### Overview
```JSON
{
  "IncidentID":"abcd",
  "PolicyName":"add_sql_replica",
  "State":"open",
  "StartedAt": 100000,
  "ClosedTimestamp":0,
  "Condition":{
    "IncidentID":"abcd",
    "PolicyName":"add_sql_replica"
  },
  "SqlMasterInstance":"sql-development",
  "ReplicaBaseName":"sql-development-",
  "Documentation":{
    "Content":"json blob here",
    "MimeType":"markdown"
  },
  "InProgress":true,
  "Action":"add"
}
```
### Parameters
* IncidentID = string, GCF replaces all `.` with `-`
* PolicyName = string, the alert name
* State = string, whether this is an open incident or not.
* StartedAt = int64, timestamp of when this incident started
* ClosedTimestamp = int64, timestamp of when this incident ended
* Condition = object, contains data replicated at the top level
* Documentation = object
  * MimeType = string, not really needed
  * Content = Documentation from the alert, contains database and alert metadata as a json encoded string.
  ```json
    { 
      "replica_basename":"sql-development-", 
      "sql_master_instance":"sql-development", 
      "action":"add" 
    }
  ```
* SqlMasterInstance = string, Stored from the documentation.content object, contains the immutable writer instance
* ReplicaBasename = string, stored from documentation.content object, contains the base name used for the db instance
* InProgress = bool, used for incident locking in the future
* Action = string, stored from documentation.content object, this used to be PolicyName, however if we aim to extend this to other databases, we can't do that.


## Chester-Daemon 
How it works:

1. Connects to PubSub/Datastore/K8S
1. Listens to events on PubSub
1. If the event is an add event
    1. Get the event data
    1. Generate a new instance
    1. Get that instances IP address
    1. Add to proxysql config
    1. If event is closed, call it a day, else repeat
1. If the event is remove
    1. Get the event data
    1. Find a chester generated instance
    1. Get the private IP
    1. Remove that from proxysql config
    1. Remove that instance from CloudSQL
    1. If the event is closed, call it a day, else repeat


## Chester-API
HTTP Layer used for updating database configurations in a programatic way, cause I'm not manually redeploying every time we add a DB.

## Arch Diagram
![](docs/Chester%20Architecture%20-%20K8S.jpeg)


## Todo
* Add support for mounting CES/PEM keys to the proxysql containers - pending proxysql to add support to individual read replica SSLs