package main

import (
	"context"
	"fmt"
	v1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// getConfigMap gets the configmap based on label key/value pairs and the namespace
func getConfigMap(labelName, labelValue, namespace string) (apiv1.ConfigMap, error) {
	listOpts := metav1.ListOptions{LabelSelector: labelName}
	configmaps, err := kubeClient.CoreV1().ConfigMaps(namespace).List(context.TODO(), listOpts)
	if err != nil {
		return apiv1.ConfigMap{}, err
	}
	for _, cf := range configmaps.Items {
		if val, ok := cf.Labels[labelName]; ok {
			if val == labelValue {
				return cf, nil
			}
		}
	}
	// kinda hacky for the moment
	return apiv1.ConfigMap{}, errors.NewNotFound(schema.GroupResource{}, "configmap")
}

// getDeployment gets a deployment based on the label key/value pairs and the namespace
func getDeployment(labelName, labelValue, namespace string) (v1.Deployment, error) {
	listOpts := metav1.ListOptions{LabelSelector: labelName}
	deployments, err := kubeClient.AppsV1().Deployments(namespace).List(context.TODO(), listOpts)
	if err != nil {
		return v1.Deployment{}, err
	}
	for _, deploy := range deployments.Items {
		if val, ok := deploy.Labels[labelName]; ok {
			if val == labelValue {
				return deploy, nil
			}
		}
	}
	return v1.Deployment{}, fmt.Errorf("no deployment with label %s and value %s exists", labelName, labelValue)
}
