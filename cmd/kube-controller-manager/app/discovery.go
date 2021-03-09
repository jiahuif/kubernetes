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
	discoveryv1beta1 "k8s.io/api/discovery/v1beta1"
	"k8s.io/controller-manager/controller"
	cmerrors "k8s.io/controller-manager/controller/errors"
	"k8s.io/klog/v2"
	endpointslicecontroller "k8s.io/kubernetes/pkg/controller/endpointslice"
	endpointslicemirroringcontroller "k8s.io/kubernetes/pkg/controller/endpointslicemirroring"
)

func startEndpointSliceController(ctx ControllerContext) (controller.Interface, error) {
	c := endpointslicecontroller.NewController(
		ctx.InformerFactory.Core().V1().Pods(),
		ctx.InformerFactory.Core().V1().Services(),
		ctx.InformerFactory.Core().V1().Nodes(),
		ctx.InformerFactory.Discovery().V1().EndpointSlices(),
		ctx.ComponentConfig.EndpointSliceController.MaxEndpointsPerSlice,
		ctx.ClientBuilder.ClientOrDie("endpointslice-controller"),
		ctx.ComponentConfig.EndpointSliceController.EndpointUpdatesBatchPeriod.Duration,
	)
	go c.Run(int(ctx.ComponentConfig.EndpointSliceController.ConcurrentServiceEndpointSyncs), ctx.Stop)
	return c, nil
}

func startEndpointSliceMirroringController(ctx ControllerContext) (controller.Interface, error) {
	if !ctx.AvailableResources[discoveryv1beta1.SchemeGroupVersion.WithResource("endpointslices")] {
		klog.Warningf("Not starting endpointslicemirroring-controller since discovery.k8s.io/v1beta1 resources are not available")
		return nil, cmerrors.ErrNotEnabled
	}

	c := endpointslicemirroringcontroller.NewController(
		ctx.InformerFactory.Core().V1().Endpoints(),
		ctx.InformerFactory.Discovery().V1().EndpointSlices(),
		ctx.InformerFactory.Core().V1().Services(),
		ctx.ComponentConfig.EndpointSliceMirroringController.MirroringMaxEndpointsPerSubset,
		ctx.ClientBuilder.ClientOrDie("endpointslicemirroring-controller"),
		ctx.ComponentConfig.EndpointSliceMirroringController.MirroringEndpointUpdatesBatchPeriod.Duration,
	)
	go c.Run(int(ctx.ComponentConfig.EndpointSliceMirroringController.MirroringConcurrentServiceEndpointSyncs), ctx.Stop)
	return c, nil
}
