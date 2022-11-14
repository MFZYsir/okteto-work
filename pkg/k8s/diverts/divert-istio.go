// Copyright 2022 The Okteto Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package diverts

import (
	"context"

	oktetoErrors "github.com/okteto/okteto/pkg/errors"
	"github.com/okteto/okteto/pkg/k8s/virtualservices"
	"github.com/okteto/okteto/pkg/model"
	istioV1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	istioclientset "istio.io/client-go/pkg/clientset/versioned"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
)

// DivertVirtualService divert a virtual service to the developer namespace
func DivertVirtualService(ctx context.Context, m *model.Manifest, fromVS *istioV1beta1.VirtualService, c *istioclientset.Clientset) error {
	vs, err := virtualservices.Get(ctx, fromVS.Name, m.Namespace, c)
	if err != nil {
		if !oktetoErrors.IsNotFound(err) {
			return err
		}
		vs = translateVirtualServiceEntrypoint(m, fromVS)
		if err := virtualservices.Create(ctx, vs, c); err != nil {
			if !k8sErrors.IsAlreadyExists(err) {
				return err
			}
		}
		return nil
	}

	if vs.Annotations[model.OktetoAutoCreateAnnotation] == "true" {
		resourceVersion := vs.ResourceVersion
		vs := translateVirtualServiceEntrypoint(m, fromVS)
		vs.ResourceVersion = resourceVersion
	} else {
		vs = updateVirtualServiceEntrypoint(m, vs)
	}

	if err := virtualservices.Update(ctx, vs, c); err != nil {
		return err
	}

	fromVS = translateVirtualService(m, fromVS, true)
	return virtualservices.Update(ctx, fromVS, c)
}

// UndoDivertVirtualService divert a virtual service to the developer namespace
func UndoDivertVirtualService(ctx context.Context, m *model.Manifest, name string, c *istioclientset.Clientset) error {
	vs, err := virtualservices.Get(ctx, name, m.Deploy.Divert.Namespace, c)
	if err != nil {
		return err
	}

	if !oktetoErrors.IsNotFound(err) {
		return err
	}
	vs = translateVirtualService(m, vs, false)
	if err := virtualservices.Update(ctx, vs, c); err != nil {
		return err
	}
	return nil
}
