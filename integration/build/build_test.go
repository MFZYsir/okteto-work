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

package build

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/okteto/okteto/integration"
	"github.com/okteto/okteto/integration/commands"
	"github.com/okteto/okteto/pkg/model"
	"github.com/okteto/okteto/pkg/okteto"
	"github.com/okteto/okteto/pkg/registry"
	"github.com/stretchr/testify/require"
)

var (
	user          = ""
	kubectlBinary = "kubectl"
	appsSubdomain = "cloud.okteto.net"
)

const (
	manifestName    = "okteto.yml"
	manifestContent = `
build:
  app:
    context: .
  api:
    context: .
    dockerfile: Dockerfile
`
	composeName    = "docker-compose.yml"
	composeContent = `
services:
  vols:
    build: .
    volumes:
    - Dockerfile:/root/Dockerfile
`
	dockerfileName    = "Dockerfile"
	dockerfileContent = "FROM alpine"
)

func TestMain(m *testing.M) {
	if u, ok := os.LookupEnv(model.OktetoUserEnvVar); !ok {
		log.Println("OKTETO_USER is not defined")
		os.Exit(1)
	} else {
		user = u
	}

	if v := os.Getenv(model.OktetoAppsSubdomainEnvVar); v != "" {
		appsSubdomain = v
	}

	if runtime.GOOS == "windows" {
		kubectlBinary = "kubectl.exe"
	}

	originalNamespace := integration.GetCurrentNamespace()

	exitCode := m.Run()

	oktetoPath, _ := integration.GetOktetoPath()
	commands.RunOktetoNamespace(oktetoPath, originalNamespace)
	os.Exit(exitCode)
}

func TestBuildCommandV1(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, createDockerfile(dir))

	oktetoPath, err := integration.GetOktetoPath()
	require.NoError(t, err)

	testNamespace := integration.GetTestNamespace("TestBuild", user)
	require.NoError(t, commands.RunOktetoCreateNamespace(oktetoPath, testNamespace))
	defer commands.RunOktetoDeleteNamespace(oktetoPath, testNamespace)

	expectedImage := fmt.Sprintf("%s/%s/test:okteto", okteto.Context().Registry, testNamespace)
	require.False(t, isImageBuilt(expectedImage))

	options := &commands.BuildOptions{
		Workdir:      dir,
		ManifestPath: filepath.Join(dir, dockerfileName),
		Tag:          "okteto.dev/test:okteto",
	}
	require.NoError(t, commands.RunOktetoBuild(oktetoPath, options))
	require.True(t, isImageBuilt(expectedImage))
}

func TestBuildCommandV2(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, createDockerfile(dir))
	require.NoError(t, createManifestV2(dir))

	oktetoPath, err := integration.GetOktetoPath()
	require.NoError(t, err)

	testNamespace := integration.GetTestNamespace("TestBuild", user)
	require.NoError(t, commands.RunOktetoCreateNamespace(oktetoPath, testNamespace))
	defer commands.RunOktetoDeleteNamespace(oktetoPath, testNamespace)

	expectedAppImage := fmt.Sprintf("%s/%s/%s-app:okteto", okteto.Context().Registry, testNamespace, filepath.Base(dir))
	require.False(t, isImageBuilt(expectedAppImage))

	expectedApiImage := fmt.Sprintf("%s/%s/%s-api:okteto", okteto.Context().Registry, testNamespace, filepath.Base(dir))
	require.False(t, isImageBuilt(expectedApiImage))

	options := &commands.BuildOptions{
		Workdir:      dir,
		ManifestPath: filepath.Join(dir, manifestName),
	}
	require.NoError(t, commands.RunOktetoBuild(oktetoPath, options))
	require.True(t, isImageBuilt(expectedAppImage))
	require.True(t, isImageBuilt(expectedApiImage))
}

func TestBuildCommandV2OnlyOneService(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, createDockerfile(dir))
	require.NoError(t, createManifestV2(dir))

	oktetoPath, err := integration.GetOktetoPath()
	require.NoError(t, err)

	testNamespace := integration.GetTestNamespace("TestBuild", user)
	require.NoError(t, commands.RunOktetoCreateNamespace(oktetoPath, testNamespace))
	defer commands.RunOktetoDeleteNamespace(oktetoPath, testNamespace)

	expectedImage := fmt.Sprintf("%s/%s/%s-app:okteto", okteto.Context().Registry, testNamespace, filepath.Base(dir))
	require.False(t, isImageBuilt(expectedImage))

	options := &commands.BuildOptions{
		Workdir:      dir,
		ManifestPath: filepath.Join(dir, manifestName),
		SvcsToBuild:  []string{"app"},
	}
	require.NoError(t, commands.RunOktetoBuild(oktetoPath, options))
	require.True(t, isImageBuilt(expectedImage))
}

func TestBuildCommandV2SpecifyingServices(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, createDockerfile(dir))
	require.NoError(t, createManifestV2(dir))

	oktetoPath, err := integration.GetOktetoPath()
	require.NoError(t, err)

	testNamespace := integration.GetTestNamespace("TestBuild", user)
	require.NoError(t, commands.RunOktetoCreateNamespace(oktetoPath, testNamespace))
	defer commands.RunOktetoDeleteNamespace(oktetoPath, testNamespace)

	expectedAppImage := fmt.Sprintf("%s/%s/%s-app:okteto", okteto.Context().Registry, testNamespace, filepath.Base(dir))
	require.False(t, isImageBuilt(expectedAppImage))

	expectedApiImage := fmt.Sprintf("%s/%s/%s-api:okteto", okteto.Context().Registry, testNamespace, filepath.Base(dir))
	require.False(t, isImageBuilt(expectedApiImage))

	options := &commands.BuildOptions{
		Workdir:      dir,
		ManifestPath: filepath.Join(dir, manifestName),
		SvcsToBuild:  []string{"app", "api"},
	}
	require.NoError(t, commands.RunOktetoBuild(oktetoPath, options))
	require.True(t, isImageBuilt(expectedAppImage))
	require.True(t, isImageBuilt(expectedApiImage))
}

func TestBuildCommandV2VolumeMounts(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, createDockerfile(dir))
	require.NoError(t, createCompose(dir))

	oktetoPath, err := integration.GetOktetoPath()
	require.NoError(t, err)

	testNamespace := integration.GetTestNamespace("TestBuild", user)
	require.NoError(t, commands.RunOktetoCreateNamespace(oktetoPath, testNamespace))
	defer commands.RunOktetoDeleteNamespace(oktetoPath, testNamespace)

	expectedBuildImage := fmt.Sprintf("%s/%s/%s-vols:okteto", okteto.Context().Registry, testNamespace, filepath.Base(dir))
	require.False(t, isImageBuilt(expectedBuildImage))

	expectedImageWithVolumes := fmt.Sprintf("%s/%s/%s-vols:okteto-with-volume-mounts", okteto.Context().Registry, testNamespace, filepath.Base(dir))
	require.False(t, isImageBuilt(expectedImageWithVolumes))

	options := &commands.BuildOptions{
		Workdir: dir,
	}
	require.NoError(t, commands.RunOktetoBuild(oktetoPath, options))
	require.True(t, isImageBuilt(expectedBuildImage), "%s not found", expectedBuildImage)
	require.True(t, isImageBuilt(expectedImageWithVolumes), "%s not found", expectedImageWithVolumes)
}

func createDockerfile(dir string) error {
	dockerfilePath := filepath.Join(dir, dockerfileName)
	dockerfileContent := []byte(dockerfileContent)
	if err := os.WriteFile(dockerfilePath, dockerfileContent, 0644); err != nil {
		return err
	}
	return nil
}

func createManifestV2(dir string) error {
	manifestPath := filepath.Join(dir, manifestName)
	manifestBytes := []byte(manifestContent)
	if err := os.WriteFile(manifestPath, manifestBytes, 0644); err != nil {
		return err
	}
	return nil
}

func createCompose(dir string) error {
	manifestPath := filepath.Join(dir, composeName)
	manifestBytes := []byte(composeContent)
	if err := os.WriteFile(manifestPath, manifestBytes, 0644); err != nil {
		return err
	}
	return nil
}

func isImageBuilt(image string) bool {
	reg := registry.NewOktetoRegistry()
	if _, err := reg.GetImageTagWithDigest(image); err == nil {
		return true
	}
	return false
}