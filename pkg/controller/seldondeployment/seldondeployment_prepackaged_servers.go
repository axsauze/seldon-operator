/*
Copyright 2019 The Seldon Team.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package seldondeployment

import (
	"encoding/json"
	machinelearningv1alpha2 "github.com/seldonio/seldon-operator/pkg/apis/machinelearning/v1alpha2"
	"github.com/seldonio/seldon-operator/pkg/constants"
	"github.com/seldonio/seldon-operator/pkg/utils"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"
	"strings"
)

var (
	DefaultSKLearnServerImageNameRest = "seldonio/sklearnserver_rest:0.1"
	DefaultSKLearnServerImageNameGrpc = "seldonio/sklearnserver_grpc:0.1"
	DefaultXGBoostServerImageNameRest = "seldonio/xgboostserver_rest:0.1"
	DefaultXGBoostServerImageNameGrpc = "seldonio/xgboostserver_grpc:0.1"
	DefaultTFServerImageNameRest      = "seldonio/tfserving-proxy_rest:0.3"
	DefaultTFServerImageNameGrpc      = "seldonio/tfserving-proxy_grpc:0.3"
	TFServingContainerName            = "tfserving"
)

func addTFServerContainer(r *ReconcileSeldonDeployment, pu *machinelearningv1alpha2.PredictiveUnit, p *machinelearningv1alpha2.PredictorSpec, deploy *appsv1.Deployment) error {

	if *pu.Implementation == machinelearningv1alpha2.TENSORFLOW_SERVER {

		ty := machinelearningv1alpha2.MODEL
		pu.Type = &ty

		if pu.Endpoint == nil {
			pu.Endpoint = &machinelearningv1alpha2.Endpoint{Type: machinelearningv1alpha2.REST}
		}

		c := utils.GetContainerForDeployment(deploy, pu.Name)
		existing := c != nil
		if !existing {
			c = &v1.Container{
				Name: pu.Name,
			}
		}

		var uriParam machinelearningv1alpha2.Parameter
		//Add missing fields
		// Add image
		if c.Image == "" {
			if pu.Endpoint.Type == machinelearningv1alpha2.REST {
				c.Image = DefaultTFServerImageNameRest
				uriParam = machinelearningv1alpha2.Parameter{
					Name:  "rest_endpoint",
					Type:  "STRING",
					Value: "http://0.0.0.0:2001",
				}
			} else {
				c.Image = DefaultTFServerImageNameGrpc
				uriParam = machinelearningv1alpha2.Parameter{
					Name:  "grpc_endpoint",
					Type:  "STRING",
					Value: "0.0.0.0:2000",
				}

			}
			c.ImagePullPolicy = v1.PullAlways
		}

		parameters := append(pu.Parameters, uriParam)
		if len(parameters) > 0 {
			if !utils.HasEnvVar(c.Env, machinelearningv1alpha2.ENV_PREDICTIVE_UNIT_PARAMETERS) {
				c.Env = append(c.Env, v1.EnvVar{Name: machinelearningv1alpha2.ENV_PREDICTIVE_UNIT_PARAMETERS, Value: utils.GetPredictiveUnitAsJson(parameters)})
			}
		}

		addContainerDefaults(c)

		// Add container to deployment
		if !existing {
			if len(deploy.Spec.Template.Spec.Containers) > 0 {
				deploy.Spec.Template.Spec.Containers = append(deploy.Spec.Template.Spec.Containers, *c)
			} else {
				deploy.Spec.Template.Spec.Containers = []v1.Container{*c}
			}
		}

		tfServingContainer := utils.GetContainerForDeployment(deploy, TFServingContainerName)
		existing = tfServingContainer != nil
		if !existing {
			tfServingContainer = &v1.Container{
				Name:  TFServingContainerName,
				Image: "tensorflow/serving:latest",
				Args: []string{
					"/usr/bin/tensorflow_model_server",
					"--port=2000",
					"--rest_api_port=2001",
					"--model_name=" + pu.Name,
					"--model_base_path=" + DefaultModelLocalMountPath},
				ImagePullPolicy: v1.PullIfNotPresent,
				Ports: []v1.ContainerPort{
					{
						ContainerPort: 2000,
						Protocol:      v1.ProtocolTCP,
					},
					{
						ContainerPort: 2001,
						Protocol:      v1.ProtocolTCP,
					},
				},
			}
		}

		addContainerDefaults(tfServingContainer)

		if !existing {
			deploy.Spec.Template.Spec.Containers = append(deploy.Spec.Template.Spec.Containers, *tfServingContainer)
		}

		_, err := InjectModelInitializer(deploy, tfServingContainer.Name, pu.ModelURI, pu.ServiceAccountName, pu.EnvSecretRefName, r)
		if err != nil {
			return err
		}
	}
	return nil
}

func addModelDefaultServers(r *ReconcileSeldonDeployment, pu *machinelearningv1alpha2.PredictiveUnit, p *machinelearningv1alpha2.PredictorSpec, deploy *appsv1.Deployment) error {
	if *pu.Implementation == machinelearningv1alpha2.SKLEARN_SERVER ||
		*pu.Implementation == machinelearningv1alpha2.XGBOOST_SERVER {

		ty := machinelearningv1alpha2.MODEL
		pu.Type = &ty

		if pu.Endpoint == nil {
			pu.Endpoint = &machinelearningv1alpha2.Endpoint{Type: machinelearningv1alpha2.REST}
		}
		c := utils.GetContainerForDeployment(deploy, pu.Name)
		existing := c != nil
		if !existing {
			c = &v1.Container{
				Name: pu.Name,
			}
		}

		//Add missing fields
		// Add image
		if c.Image == "" {
			if *pu.Implementation == machinelearningv1alpha2.SKLEARN_SERVER {
				if pu.Endpoint.Type == machinelearningv1alpha2.REST {
					c.Image = DefaultSKLearnServerImageNameRest
				} else {
					c.Image = DefaultSKLearnServerImageNameGrpc
				}
			} else if *pu.Implementation == machinelearningv1alpha2.XGBOOST_SERVER {
				if pu.Endpoint.Type == machinelearningv1alpha2.REST {
					c.Image = DefaultXGBoostServerImageNameRest
				} else {
					c.Image = DefaultXGBoostServerImageNameGrpc
				}
			}
		}

		// Add parameters envvar - point at mount path because initContainer will download
		if !utils.HasEnvVar(c.Env, constants.PU_PARAMETER_ENVVAR) {
			params := pu.Parameters
			uriParam := machinelearningv1alpha2.Parameter{
				Name:  "model_uri",
				Type:  "STRING",
				Value: DefaultModelLocalMountPath,
			}
			params = append(params, uriParam)
			paramStr, err := json.Marshal(params)
			if err != nil {
				return err
			}
			addContainerDefaults(c)

			c.Env = append(c.Env, corev1.EnvVar{Name: constants.PU_PARAMETER_ENVVAR, Value: string(paramStr)})
		}

		// Add container to deployment
		if !existing {
			if len(deploy.Spec.Template.Spec.Containers) > 0 {
				deploy.Spec.Template.Spec.Containers = append(deploy.Spec.Template.Spec.Containers, *c)
			} else {
				deploy.Spec.Template.Spec.Containers = []v1.Container{*c}
			}
		}

		_, err := InjectModelInitializer(deploy, c.Name, pu.ModelURI, pu.ServiceAccountName, pu.EnvSecretRefName, r.Client)
		if err != nil {
			return err
		}
	}
	return nil
}

func addContainerDefaults(c *v1.Container) {
	// Add some defaults for easier diffs
	if c.TerminationMessagePath == "" {
		c.TerminationMessagePath = "/dev/termination-log"
	}
	if c.TerminationMessagePolicy == "" {
		c.TerminationMessagePolicy = corev1.TerminationMessageReadFile
	}

	if c.ImagePullPolicy == "" {
		c.ImagePullPolicy = corev1.PullIfNotPresent
	}
}

func createStandaloneModelServers(r *ReconcileSeldonDeployment, mlDep *machinelearningv1alpha2.SeldonDeployment, p *machinelearningv1alpha2.PredictorSpec, c *components, pu *machinelearningv1alpha2.PredictiveUnit) error {

	// some predictors have no podSpec so this could be nil
	sPodSpec := utils.GetSeldonPodSpecForPredictiveUnit(p, pu.Name)

	depName := machinelearningv1alpha2.GetDeploymentName(mlDep, *p, sPodSpec)

	var deploy *appsv1.Deployment
	existing := false
	for i := 0; i < len(c.deployments); i++ {
		d := c.deployments[i]
		if strings.Compare(d.Name, depName) == 0 {
			deploy = d
			existing = true
			break
		}
	}

	// might not be a Deployment yet - if so we have to create one
	if deploy == nil {
		seldonId := machinelearningv1alpha2.GetSeldonDeploymentName(mlDep)
		deploy = createDeploymentWithoutEngine(depName, seldonId, sPodSpec, p, mlDep)
	}

	if err := addModelDefaultServers(r, pu, p, deploy); err != nil {
		return err
	}
	if err := addTFServerContainer(r, pu, p, deploy); err != nil {
		return err
	}

	if !existing {

		// this is a new deployment so its containers won't have a containerService
		for k := 0; k < len(deploy.Spec.Template.Spec.Containers); k++ {
			con := &deploy.Spec.Template.Spec.Containers[k]

			//checking for con.Name != "" is a fallback check that we haven't got an empty/nil container as name is required
			if con.Name != EngineContainerName && con.Name != TFServingContainerName && con.Name != "" {
				svc := createContainerService(deploy, *p, mlDep, con, *c)
				c.services = append(c.services, svc)
			}
		}
		if len(deploy.Spec.Template.Spec.Containers) > 0 && deploy.Spec.Template.Spec.Containers[0].Name != "" {
			// Add deployment, provided we have a non-empty spec
			c.deployments = append(c.deployments, deploy)
		}
	}

	for i := 0; i < len(pu.Children); i++ {
		if err := createStandaloneModelServers(r, mlDep, p, c, &pu.Children[i]); err != nil {
			return err
		}
	}
	return nil
}
