/*
Copyright 2016 The Kubernetes Authors.

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

// Package app implements a server that runs a set of active
// components.  This includes replication controllers, service endpoints and
// nodes.
//
package app

import (
	"fmt"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	cmcontroller "k8s.io/controller-manager/controller"
	cmerrors "k8s.io/controller-manager/controller/errors"
	"k8s.io/kubernetes/pkg/controller/cronjob"
	"k8s.io/kubernetes/pkg/controller/job"
	kubefeatures "k8s.io/kubernetes/pkg/features"
)

func startJobController(ctx ControllerContext) (cmcontroller.Controller, error) {
	if !ctx.AvailableResources[schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}] {
		return nil, cmerrors.ErrNotEnabled
	}
	c := job.NewController(
		ctx.InformerFactory.Core().V1().Pods(),
		ctx.InformerFactory.Batch().V1().Jobs(),
		ctx.ClientBuilder.ClientOrDie("job-controller"),
	)
	go c.Run(int(ctx.ComponentConfig.JobController.ConcurrentJobSyncs), ctx.Stop)
	return c, nil
}

func startCronJobController(ctx ControllerContext) (cmcontroller.Controller, error) {
	if !ctx.AvailableResources[schema.GroupVersionResource{Group: "batch", Version: "v1beta1", Resource: "cronjobs"}] {
		return nil, cmerrors.ErrNotEnabled
	}
	if utilfeature.DefaultFeatureGate.Enabled(kubefeatures.CronJobControllerV2) {
		cj2c, err := cronjob.NewControllerV2(ctx.InformerFactory.Batch().V1().Jobs(),
			ctx.InformerFactory.Batch().V1beta1().CronJobs(),
			ctx.ClientBuilder.ClientOrDie("cronjob-controller"),
		)
		if err != nil {
			return nil, fmt.Errorf("error creating CronJob controller V2: %v", err)
		}
		go cj2c.Run(int(ctx.ComponentConfig.CronJobController.ConcurrentCronJobSyncs), ctx.Stop)
		return cj2c, nil
	}
	cjc, err := cronjob.NewController(
		ctx.ClientBuilder.ClientOrDie("cronjob-controller"),
	)
	if err != nil {
		return nil, fmt.Errorf("error creating CronJob controller: %v", err)
	}
	go cjc.Run(ctx.Stop)
	return cjc, nil
}
