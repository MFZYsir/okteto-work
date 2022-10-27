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

package context

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/okteto/okteto/cmd/utils"
	"github.com/okteto/okteto/pkg/config"
	"github.com/okteto/okteto/pkg/constants"
	oktetoContext "github.com/okteto/okteto/pkg/context"
	"github.com/okteto/okteto/pkg/discovery"
	oktetoErrors "github.com/okteto/okteto/pkg/errors"
	"github.com/okteto/okteto/pkg/k8s/kubeconfig"
	oktetoLog "github.com/okteto/okteto/pkg/log"
	"github.com/okteto/okteto/pkg/model"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

type SelectItem struct {
	Name   string
	Enable bool
}

type ManifestOptions struct {
	Name       string
	Namespace  string
	Filename   string
	K8sContext string
}

func getKubernetesContextList(filterOkteto bool) []string {
	contextList := make([]string, 0)
	kubeconfigFile := config.GetKubeconfigPath()
	cfg := kubeconfig.Get(kubeconfigFile)
	if cfg == nil {
		return contextList
	}
	if !filterOkteto {
		for name := range cfg.Contexts {
			contextList = append(contextList, name)
		}
		return contextList
	}
	for name := range cfg.Contexts {
		if _, ok := cfg.Contexts[name].Extensions[constants.OktetoExtension]; ok && filterOkteto {
			continue
		}
		contextList = append(contextList, name)
	}
	return contextList
}

func getKubernetesContextNamespace(k8sContext string) string {
	kubeconfigFile := config.GetKubeconfigPath()
	cfg := kubeconfig.Get(kubeconfigFile)
	if cfg == nil {
		return ""
	}
	if cfg.Contexts[k8sContext].Namespace == "" {
		return "default"
	}
	return cfg.Contexts[k8sContext].Namespace
}

func isCreateNewContextOption(option string) bool {
	return option == newOEOption
}

func askForOktetoURL() (string, error) {
	clusterURL := constants.CloudURL
	ctxStore := oktetoContext.ContextStore()
	if oCtx, ok := ctxStore.Contexts[ctxStore.CurrentContext]; ok && oCtx.IsOkteto {
		clusterURL = ctxStore.CurrentContext
	}

	err := oktetoLog.Question("Enter your Okteto URL [%s]: ", clusterURL)
	if err != nil {
		return "", err
	}
	fmt.Scanln(&clusterURL)

	url, err := url.Parse(clusterURL)
	if err != nil {
		return "", nil
	}
	if url.Scheme == "" {
		url.Scheme = "https"
	}
	return strings.TrimSuffix(url.String(), "/"), nil
}

func isValidCluster(cluster string) bool {
	for _, c := range getKubernetesContextList(false) {
		if cluster == c {
			return true
		}
	}
	return false
}

func addKubernetesContext(cfg *clientcmdapi.Config, ctxResource *model.ContextResource) error {
	if cfg == nil {
		return fmt.Errorf(oktetoErrors.ErrKubernetesContextNotFound, ctxResource.Context, config.GetKubeconfigPath())
	}
	if _, ok := cfg.Contexts[ctxResource.Context]; !ok {
		return fmt.Errorf(oktetoErrors.ErrKubernetesContextNotFound, ctxResource.Context, config.GetKubeconfigPath())
	}
	if ctxResource.Namespace == "" {
		ctxResource.Namespace = cfg.Contexts[ctxResource.Context].Namespace
	}
	if ctxResource.Namespace == "" {
		ctxResource.Namespace = "default"
	}
	oktetoContext.AddKubernetesContext(ctxResource.Context, ctxResource.Namespace, "")
	return nil
}

func getCtxResource(path string) (*model.ContextResource, error) {
	ctxResource, err := model.GetContextResource(path)
	if err != nil {
		if !errors.Is(err, discovery.ErrOktetoManifestNotFound) {
			return nil, err
		}

		ctxResource = &model.ContextResource{}
	}

	return ctxResource, nil
}

// LoadManifestWithContext loads context and then loads a manifest
func LoadManifestWithContext(ctx context.Context, opts ManifestOptions) (*model.Manifest, error) {
	ctxResource, err := model.GetContextResource(opts.Filename)
	if err != nil {
		return nil, err
	}
	if err := ctxResource.UpdateNamespace(opts.Namespace); err != nil {
		return nil, err
	}

	if err := ctxResource.UpdateContext(opts.K8sContext); err != nil {
		return nil, err
	}

	ctxOptions := &ContextOptions{
		Context:   ctxResource.Context,
		Namespace: ctxResource.Namespace,
		Show:      true,
	}

	if err := NewContextCommand().Run(ctx, ctxOptions); err != nil {
		return nil, err
	}

	manifest, err := model.GetManifestV1(opts.Filename)
	if err != nil {
		if !errors.Is(err, discovery.ErrOktetoManifestNotFound) {
			return nil, err
		}
		manifest, err = model.GetManifestV2(opts.Filename)
		if err != nil {
			return nil, err
		}
	}

	manifest.Namespace = oktetoContext.Context().Namespace
	manifest.Context = oktetoContext.Context().Name

	for _, dev := range manifest.Dev {
		if err := utils.LoadManifestRc(dev); err != nil {
			return nil, err
		}

		dev.Namespace = oktetoContext.Context().Namespace
		dev.Context = oktetoContext.Context().Name
	}

	return manifest, nil
}

func LoadStackWithContext(ctx context.Context, name, namespace string, stackPaths []string) (*model.Stack, error) {
	ctxResource, err := utils.LoadStackContext(stackPaths)
	if err != nil {
		if name == "" {
			return nil, err
		}
		ctxResource = &model.ContextResource{}
	}

	if err := ctxResource.UpdateNamespace(namespace); err != nil {
		return nil, err
	}

	ctxOptions := &ContextOptions{
		Context:   ctxResource.Context,
		Namespace: ctxResource.Namespace,
		Show:      true,
	}

	if err := NewContextCommand().Run(ctx, ctxOptions); err != nil {
		return nil, err
	}

	s, err := model.LoadStack(name, stackPaths, true)
	if err != nil {
		if name == "" {
			return nil, err
		}
		s = &model.Stack{Name: name}
	}
	s.Namespace = oktetoContext.Context().Namespace
	return s, nil
}

// LoadContextFromPath initializes the okteto context taking into account command flags and manifest namespace/context fields
func LoadContextFromPath(ctx context.Context, namespace, k8sContext, path string) error {
	ctxResource, err := getCtxResource(path)
	if err != nil {
		return err
	}

	if err := ctxResource.UpdateNamespace(namespace); err != nil {
		return err
	}

	if err := ctxResource.UpdateContext(k8sContext); err != nil {
		return err
	}

	ctxOptions := &ContextOptions{
		Context:   ctxResource.Context,
		Namespace: ctxResource.Namespace,
		Show:      true,
	}

	return NewContextCommand().Run(ctx, ctxOptions)
}
