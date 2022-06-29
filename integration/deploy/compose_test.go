//go:build integration
// +build integration

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

package deploy

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/okteto/okteto/integration"
	"github.com/okteto/okteto/integration/commands"
	"github.com/okteto/okteto/pkg/okteto"
	"github.com/okteto/okteto/pkg/registry"
	"github.com/stretchr/testify/require"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
)

const (
	composeTemplate = `services:
  app:
    build: app
    entrypoint: python -m http.server 8080
    ports:
      - 8080
      - 8913
  nginx:
    image: nginx
    volumes:
      - ./nginx/nginx.conf:/tmp/nginx.conf
    entrypoint: /bin/bash -c "envsubst < /tmp/nginx.conf > /etc/nginx/conf.d/default.conf && nginx -g 'daemon off;'"
    environment:
      - FLASK_SERVER_ADDR=app:8080
    ports:
      - 81:80
    depends_on:
      app:
        condition: service_started
    container_name: web-svc
    healthcheck:
      test: service nginx status || exit 1
      interval: 45s
      timeout: 5m
      retries: 5
      start_period: 30s`

	stacksTemplate = `services:
  app:
    build: app
    command: python -m http.server 8080
    ports:
    - 8080
    environment:
    - RABBITMQ_PASS
  nginx:
    image: nginx
    volumes:
    - ./nginx/nginx.conf:/tmp/nginx.conf
    command: /bin/bash -c "envsubst < /tmp/nginx.conf > /etc/nginx/conf.d/default.conf && nginx -g 'daemon off;'"
    environment:
    - FLASK_SERVER_ADDR=app:8080
    public: true
    ports:
    - 80
    depends_on:
      app:
        condition: service_started
    container_name: web-svc
    healthcheck:
      test: service nginx status || exit 1
      interval: 45s
      timeout: 5m
      retries: 5
      start_period: 30s`
	appDockerfile = `FROM python:alpine
EXPOSE 2931`
	nginxConf = `server {
  listen 80;
  location / {
    proxy_pass http://$FLASK_SERVER_ADDR;
  }
}`
)

// TestDeployPipelineFromCompose tests the following scenario:
// - Deploying a pipeline manifest locally from a compose file
// - The endpoints generated are accessible
// - Depends on
// - Test secret injection
// - Test that port from image is imported
func TestDeployPipelineFromCompose(t *testing.T) {
	t.Parallel()
	oktetoPath, err := integration.GetOktetoPath()
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, createComposeScenario(dir))

	testNamespace := integration.GetTestNamespace("TestDeployCompose", user)
	namespaceOpts := &commands.NamespaceOptions{
		Namespace:  testNamespace,
		OktetoHome: dir,
	}
	require.NoError(t, commands.RunOktetoCreateNamespace(oktetoPath, namespaceOpts))
	defer commands.RunOktetoDeleteNamespace(oktetoPath, namespaceOpts)

	deployOptions := &commands.DeployOptions{
		Workdir:    dir,
		Namespace:  testNamespace,
		OktetoHome: dir,
	}
	require.NoError(t, commands.RunOktetoDeploy(oktetoPath, deployOptions))

	// Test that the nginx image has been created correctly
	nginxDeployment, err := integration.GetDeployment(context.Background(), testNamespace, "nginx")
	require.NoError(t, err)
	nginxImageDev := fmt.Sprintf("%s/%s/%s-nginx:okteto-with-volume-mounts", okteto.Context().Registry, testNamespace, filepath.Base(dir))
	require.Equal(t, getImageWithSHA(nginxImageDev), nginxDeployment.Spec.Template.Spec.Containers[0].Image)

	// Test that the nginx image has been created correctly
	appDeployment, err := integration.GetDeployment(context.Background(), testNamespace, "app")
	require.NoError(t, err)
	appImageDev := fmt.Sprintf("%s/%s/%s-app:okteto", okteto.Context().Registry, testNamespace, filepath.Base(dir))
	require.Equal(t, getImageWithSHA(appImageDev), appDeployment.Spec.Template.Spec.Containers[0].Image)

	// Test that the k8s services has been created correctly
	appService, err := integration.GetService(context.Background(), testNamespace, "app")
	require.NoError(t, err)
	require.Len(t, appService.Spec.Ports, 3)
	for _, p := range appService.Spec.Ports {
		require.Contains(t, []int32{8080, 8913, 2931}, p.Port)
	}
	nginxService, err := integration.GetService(context.Background(), testNamespace, "nginx")
	require.NoError(t, err)
	require.Len(t, nginxService.Spec.Ports, 2)
	for _, p := range nginxService.Spec.Ports {
		require.Contains(t, []int32{80, 81}, p.Port)
	}

	destroyOptions := &commands.DestroyOptions{
		Workdir:    dir,
		Namespace:  testNamespace,
		OktetoHome: dir,
	}
	require.NoError(t, commands.RunOktetoDestroy(oktetoPath, destroyOptions))
}

// TestDeployPipelineFromComposeOnlyOneSvc tests the following scenario:
// - Deploying a pipeline manifest locally from a compose file
// - The endpoints generated are accessible
// - Depends on
// - Test secret injection
func TestDeployPipelineFromComposeOnlyOneSvc(t *testing.T) {
	t.Parallel()
	oktetoPath, err := integration.GetOktetoPath()
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, createComposeScenario(dir))

	testNamespace := integration.GetTestNamespace("TestDeployPartialCompose", user)
	namespaceOpts := &commands.NamespaceOptions{
		Namespace:  testNamespace,
		OktetoHome: dir,
	}
	require.NoError(t, commands.RunOktetoCreateNamespace(oktetoPath, namespaceOpts))
	defer commands.RunOktetoDeleteNamespace(oktetoPath, namespaceOpts)

	deployOptions := &commands.DeployOptions{
		Workdir:          dir,
		ServicesToDeploy: []string{"app"},
		Namespace:        testNamespace,
		OktetoHome:       dir,
	}
	require.NoError(t, commands.RunOktetoDeploy(oktetoPath, deployOptions))

	// Test that the nginx image has been created correctly
	_, err = integration.GetDeployment(context.Background(), testNamespace, "nginx")
	require.True(t, k8sErrors.IsNotFound(err))

	// Test that the nginx image has been created correctly
	appDeployment, err := integration.GetDeployment(context.Background(), testNamespace, "app")
	require.NoError(t, err)
	appImageDev := fmt.Sprintf("%s/%s/%s-app:okteto", okteto.Context().Registry, testNamespace, filepath.Base(dir))
	require.Equal(t, getImageWithSHA(appImageDev), appDeployment.Spec.Template.Spec.Containers[0].Image)

	destroyOptions := &commands.DestroyOptions{
		Workdir:    dir,
		Namespace:  testNamespace,
		OktetoHome: dir,
	}
	require.NoError(t, commands.RunOktetoDestroy(oktetoPath, destroyOptions))
}

// TestDeployPipelineFromOktetoStacks tests the following scenario:
// - Deploying a pipeline manifest locally from a compose file
// - The endpoints generated are accessible
// - Depends on
// - Test secret injection
// - Test that port from image is imported
func TestDeployPipelineFromOktetoStacks(t *testing.T) {
	t.Parallel()
	oktetoPath, err := integration.GetOktetoPath()
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, createStacksScenario(dir))

	testNamespace := integration.GetTestNamespace("TestDeployStacks", user)
	namespaceOpts := &commands.NamespaceOptions{
		Namespace:  testNamespace,
		OktetoHome: dir,
	}
	require.NoError(t, commands.RunOktetoCreateNamespace(oktetoPath, namespaceOpts))
	defer commands.RunOktetoDeleteNamespace(oktetoPath, namespaceOpts)

	deployOptions := &commands.DeployOptions{
		Workdir:    dir,
		Namespace:  testNamespace,
		OktetoHome: dir,
	}
	require.NoError(t, commands.RunOktetoDeploy(oktetoPath, deployOptions))

	// Test that the nginx image has been created correctly
	nginxDeployment, err := integration.GetDeployment(context.Background(), testNamespace, "nginx")
	require.NoError(t, err)

	nginxImageDev := fmt.Sprintf("%s/%s/%s-nginx:okteto-with-volume-mounts", okteto.Context().Registry, testNamespace, filepath.Base(dir))
	require.Equal(t, getImageWithSHA(nginxImageDev), nginxDeployment.Spec.Template.Spec.Containers[0].Image)

	// Test that the nginx image has been created correctly
	appDeployment, err := integration.GetDeployment(context.Background(), testNamespace, "app")
	require.NoError(t, err)

	appImageDev := fmt.Sprintf("%s/%s/%s-app:okteto", okteto.Context().Registry, testNamespace, filepath.Base(dir))
	require.Equal(t, getImageWithSHA(appImageDev), appDeployment.Spec.Template.Spec.Containers[0].Image)

	destroyOptions := &commands.DestroyOptions{
		Workdir:    dir,
		Namespace:  testNamespace,
		OktetoHome: dir,
	}
	require.NoError(t, commands.RunOktetoDestroy(oktetoPath, destroyOptions))
}

func createComposeScenario(dir string) error {
	if err := os.Mkdir(filepath.Join(dir, "nginx"), 0700); err != nil {
		return err
	}

	nginxPath := filepath.Join(dir, "nginx", "nginx.conf")
	nginxContent := []byte(nginxConf)
	if err := os.WriteFile(nginxPath, nginxContent, 0644); err != nil {
		return err
	}

	if err := createAppDockerfile(dir); err != nil {
		return err
	}

	composePath := filepath.Join(dir, "docker-compose.yml")
	composeContent := []byte(composeTemplate)
	if err := os.WriteFile(composePath, composeContent, 0644); err != nil {
		return err
	}

	return nil
}

func createStacksScenario(dir string) error {
	if err := os.Mkdir(filepath.Join(dir, "nginx"), 0700); err != nil {
		return err
	}

	nginxPath := filepath.Join(dir, "nginx", "nginx.conf")
	nginxContent := []byte(nginxConf)
	if err := os.WriteFile(nginxPath, nginxContent, 0644); err != nil {
		return err
	}

	if err := createAppDockerfile(dir); err != nil {
		return err
	}

	composePath := filepath.Join(dir, "okteto-stack.yml")
	composeContent := []byte(stacksTemplate)
	if err := os.WriteFile(composePath, composeContent, 0644); err != nil {
		return err
	}

	return nil
}

func getImageWithSHA(devImage string) string {
	reg := registry.NewOktetoRegistry()
	tag, err := reg.GetImageTagWithDigest(devImage)
	if err != nil {
		log.Printf("could not get %s from registry", devImage)
		return ""
	}
	return tag
}

func createAppDockerfile(dir string) error {
	if err := os.Mkdir(filepath.Join(dir, "app"), 0700); err != nil {
		return err
	}

	appDockerfilePath := filepath.Join(dir, "app", "Dockerfile")
	appDockerfileContent := []byte(appDockerfile)
	if err := os.WriteFile(appDockerfilePath, appDockerfileContent, 0644); err != nil {
		return err
	}
	return nil
}
