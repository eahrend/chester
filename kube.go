package main

import (
	"context"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

// updateConfigMap updates the k8s configmap object based
// on the latest proxysql configuration in datastore
// TODO: Move this to a secret as it contains sensitive data
//  in addition we'll add software level encryption to the secrets
func updateConfigMap(instanceGroup string) error {
	psqlConfig, err := getProxySQLConfig(instanceGroup)
	if err != nil {
		return err
	}
	err = psqlConfig.DecryptPasswords(kmsClient)
	if err != nil {
		return err
	}
	todoContext := context.TODO()
	configMap, err := getConfigMap("instancegroup", instanceGroup, "proxysql")
	if err != nil {
		return err
	}
	if errors.IsNotFound(err) {
		log.Debugln("Configmap not found, not deleting anything")
	} else if err != nil {
		return err
	} else {
		err = kubeClient.CoreV1().ConfigMaps("proxysql").Delete(todoContext, configMap.Name, metav1.DeleteOptions{})
		if err != nil {
			return err
		}
	}
	b, err := psqlConfig.ToLibConfig()
	configMap = apiv1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Configmap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMap.Name,
			Namespace: "proxysql",
			Labels:    configMap.Labels,
		},
		Immutable: nil,
		// TODO: Once proxysql has the ability to do host:ssl config
		//  we can add a json object of each host's SSL config here
		//  but until then we're just not using SSL
		Data: map[string]string{
			"proxysql.cnf": string(b),
		},
		BinaryData: nil,
	}
	configMapClient := kubeClient.CoreV1().ConfigMaps("proxysql")
	_, err = configMapClient.Create(todoContext, &configMap, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	return nil
}

// reloadProxySql updates the deployment with a new env var
// to cause a rolling update.
func reloadProxySql(instanceGroup string) error {
	todoContext := context.TODO()
	deployClient := kubeClient.AppsV1().Deployments("proxysql")
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, getErr := getDeployment("instancegroup", instanceGroup, "proxysql")
		if getErr != nil {
			return getErr
		}
		refreshUUID, err := uuid.NewRandom()
		if err != nil {
			return err
		}
		envVars := result.Spec.Template.Spec.Containers[0].Env
		// pop the old refresh
		for k, v := range envVars {
			if v.Name == "refresh" {
				envVars[k] = envVars[len(envVars)-1]     // Copy last element to index i.
				envVars[len(envVars)-1] = apiv1.EnvVar{} // Erase last element (write zero value).
				envVars = envVars[:len(envVars)-1]       // Truncate slice.
				break
			}
		}
		envVar := apiv1.EnvVar{
			Name:      "refresh",
			Value:     refreshUUID.String(),
			ValueFrom: nil,
		}
		envVars = append(envVars, envVar)
		result.Spec.Template.Spec.Containers[0].Env = envVars
		_, updateErr := deployClient.Update(todoContext, &result, metav1.UpdateOptions{})
		if updateErr != nil {
			return updateErr
		}
		return updateErr
	})
	return retryErr
}
