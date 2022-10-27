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
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/okteto/okteto/pkg/config"
	"github.com/okteto/okteto/pkg/constants"
	oktetoErrors "github.com/okteto/okteto/pkg/errors"
	"github.com/okteto/okteto/pkg/k8s/ingresses"
	"github.com/okteto/okteto/pkg/k8s/kubeconfig"
	oktetoLog "github.com/okteto/okteto/pkg/log"
	"github.com/okteto/okteto/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

type k8sClientProvider interface {
	Provide(clientApiConfig *clientcmdapi.Config) (kubernetes.Interface, *rest.Config, error)
	GetIngressClient() (*ingresses.Client, error)
}

type OktetoContextStore struct {
	Contexts       map[string]*OktetoContext `json:"contexts"`
	CurrentContext string                    `json:"current-context"`
}

const (
	localClusterType  = "local"
	remoteClusterType = "remote"
)

var (
	CurrentStore  *OktetoContextStore
	reg           = regexp.MustCompile("[^A-Za-z0-9]+")
	strictTLSOnce sync.Once
)

// OktetoContext contains the information related to an okteto context
type OktetoContext struct {
	Name              string               `json:"name" yaml:"name,omitempty"`
	UserID            string               `json:"id,omitempty" yaml:"id,omitempty"`
	Username          string               `json:"username,omitempty" yaml:"username,omitempty"`
	Token             string               `json:"token,omitempty" yaml:"token,omitempty"`
	Namespace         string               `json:"namespace" yaml:"namespace,omitempty"`
	Cfg               *clientcmdapi.Config `json:"-" yaml:"-"`
	Builder           string               `json:"builder,omitempty" yaml:"builder,omitempty"`
	Registry          string               `json:"registry,omitempty" yaml:"registry,omitempty"`
	Certificate       string               `json:"certificate,omitempty" yaml:"certificate,omitempty"`
	PersonalNamespace string               `json:"personalNamespace,omitempty" yaml:"personalNamespace,omitempty"`
	GlobalNamespace   string               `json:"-" yaml:"-"`
	Analytics         bool                 `json:"-" yaml:"-"`
	ClusterType       string               `json:"-" yaml:"-"`
	IsOkteto          bool                 `json:"isOkteto,omitempty" yaml:"isOkteto,omitempty"`
}

// OktetoContextViewer contains info to show
type OktetoContextViewer struct {
	Name      string `json:"name" yaml:"name,omitempty"`
	Namespace string `json:"namespace" yaml:"namespace,omitempty"`
	Builder   string `json:"builder,omitempty" yaml:"builder,omitempty"`
	Registry  string `json:"registry,omitempty" yaml:"registry,omitempty"`
	Current   bool   `json:"current" yaml:"current"`
}

// ContextExists checks if an okteto context has been created
func ContextExists() bool {
	oktetoContextFolder := config.GetOktetoContextsStorePath()
	if _, err := os.Stat(oktetoContextFolder); err != nil {
		if os.IsNotExist(err) {
			return false
		}
		oktetoLog.Fatalf("error accessing okteto context store '%s': %s", oktetoContextFolder, err)
	}
	return true
}

func UrlToKubernetesContext(uri string) string {
	u, _ := url.Parse(uri)
	return strings.ReplaceAll(u.Host, ".", "_")
}

// K8sContextToOktetoUrl translates k8s contexts like cloud_okteto_com to hettps://cloud.okteto.com
func K8sContextToOktetoUrl(ctx context.Context, k8sContext, k8sNamespace string, clientProvider k8sClientProvider) string {
	ctxStore := ContextStore()
	// check if belongs to the okteto contexts
	for name, oCtx := range ctxStore.Contexts {
		if oCtx.IsOkteto && UrlToKubernetesContext(name) == k8sContext {
			return name
		}
	}

	cfg := kubeconfig.Get(config.GetKubeconfigPath())
	if cfg == nil {
		return k8sContext
	}

	cfg.CurrentContext = k8sContext
	c, _, err := clientProvider.Provide(cfg)
	if err != nil {
		oktetoLog.Infof("error getting k8s client: %v", err)
		return k8sContext
	}

	// check the namespace label
	if k8sNamespace == "" {
		k8sNamespace = cfg.Contexts[k8sContext].Namespace
	}

	if k8sNamespace == "" {
		return k8sContext
	}

	n, err := c.CoreV1().Namespaces().Get(ctx, k8sNamespace, metav1.GetOptions{})
	if err != nil {
		oktetoLog.Debugf("error accessing current namespace: %v", err)
		return k8sContext
	}
	if _, ok := n.Labels[constants.DevLabel]; ok {
		return n.Annotations[constants.OktetoURLAnnotation]
	}

	return k8sContext
}

func IsContextInitialized() bool {
	ctxStore := ContextStore()
	return ctxStore.CurrentContext != ""
}

func IsOkteto() bool {
	return Context().IsOkteto
}

func ContextStore() *OktetoContextStore {
	if CurrentStore != nil {
		return CurrentStore
	}

	if ContextExists() {
		b, err := os.ReadFile(config.GetOktetoContextsStorePath())
		if err != nil {
			oktetoLog.Errorf("error reading okteto contexts: %v", err)
			oktetoLog.Fatalf(oktetoErrors.ErrCorruptedOktetoContexts, config.GetOktetoContextFolder())
		}

		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields() // Force errors

		ctxStore := &OktetoContextStore{}
		if err := dec.Decode(&ctxStore); err != nil {
			oktetoLog.Errorf("error decoding okteto contexts: %v", err)
			oktetoLog.Fatalf(oktetoErrors.ErrCorruptedOktetoContexts, config.GetOktetoContextFolder())
		}
		CurrentStore = ctxStore

		if _, ok := CurrentStore.Contexts[CurrentStore.CurrentContext]; !ok {
			return CurrentStore
		}
		return CurrentStore
	}

	CurrentStore = &OktetoContextStore{
		Contexts: map[string]*OktetoContext{},
	}
	return CurrentStore
}

func Context() *OktetoContext {
	c := ContextStore()
	if c.CurrentContext == "" {
		oktetoLog.Info("ContextStore().CurrentContext is empty")
		oktetoLog.Fatalf(oktetoErrors.ErrCorruptedOktetoContexts, config.GetOktetoContextFolder())
	}
	octx, ok := c.Contexts[c.CurrentContext]
	if !ok {
		oktetoLog.Info("ContextStore().CurrentContext not in ContextStore().Contexts")
		oktetoLog.Fatalf(oktetoErrors.ErrCorruptedOktetoContexts, config.GetOktetoContextFolder())
	}

	return octx
}

func HasBeenLogged(oktetoURL string) bool {
	octxStore := ContextStore()
	_, ok := octxStore.Contexts[oktetoURL]
	return ok
}

func AddOktetoContext(name string, u *types.User, namespace, personalNamespace string) {
	CurrentStore = ContextStore()
	name = strings.TrimSuffix(name, "/")
	CurrentStore.Contexts[name] = &OktetoContext{
		Name:              name,
		UserID:            u.ID,
		Username:          u.ExternalID,
		Token:             u.Token,
		Namespace:         namespace,
		PersonalNamespace: personalNamespace,
		GlobalNamespace:   u.GlobalNamespace,
		Builder:           u.Buildkit,
		Registry:          u.Registry,
		Certificate:       u.Certificate,
		Analytics:         u.Analytics,
	}
	CurrentStore.CurrentContext = name
}

func AddKubernetesContext(name, namespace, buildkitURL string) {
	CurrentStore = ContextStore()
	CurrentStore.Contexts[name] = &OktetoContext{
		Name:      name,
		Namespace: namespace,
		Builder:   buildkitURL,
		Analytics: true,
	}
	CurrentStore.CurrentContext = name
}

type ContextConfigWriterInterface interface {
	Write() error
}

// ContextConfigWriter writes the information about the context config into stdout
type ContextConfigWriter struct{}

func NewContextConfigWriter() *ContextConfigWriter {
	return &ContextConfigWriter{}
}

func (*ContextConfigWriter) Write() error {
	marshalled, err := json.MarshalIndent(ContextStore(), "", "\t")
	if err != nil {
		oktetoLog.Infof("failed to marshal context: %s", err)
		return fmt.Errorf("failed to generate your context")
	}

	contextFolder := config.GetOktetoContextFolder()
	if err := os.MkdirAll(contextFolder, 0700); err != nil {
		oktetoLog.Fatalf("failed to create %s: %s", contextFolder, err)
	}

	contextConfigPath := config.GetOktetoContextsStorePath()
	if _, err := os.Stat(contextConfigPath); err == nil {
		err = os.Chmod(contextConfigPath, 0600)
		if err != nil {
			return fmt.Errorf("couldn't change context permissions: %s", err)
		}
	}

	if err := os.WriteFile(contextConfigPath, marshalled, 0600); err != nil {
		return fmt.Errorf("couldn't save context: %s", err)
	}

	return nil
}

func AddOktetoCredentialsToCfg(cfg *clientcmdapi.Config, cred *types.Credential, namespace, userName, oktetoURL string) {
	// If the context is being initialized within the execution of `okteto deploy` deploy command it should not
	// write the Okteto credentials into the kubeconfig. It would overwrite the proxy settings
	if os.Getenv(constants.OktetoSkipConfigCredentialsUpdate) == "true" {
		return
	}

	clusterName := UrlToKubernetesContext(oktetoURL)
	// create cluster
	cluster, ok := cfg.Clusters[clusterName]
	if !ok {
		cluster = clientcmdapi.NewCluster()
	}

	cluster.CertificateAuthorityData = []byte(cred.Certificate)
	cluster.Server = cred.Server
	cfg.Clusters[clusterName] = cluster

	// create user
	user, ok := cfg.AuthInfos[userName]
	if !ok {
		user = clientcmdapi.NewAuthInfo()
	}
	user.Token = cred.Token
	cfg.AuthInfos[userName] = user

	// create context
	context, ok := cfg.Contexts[clusterName]
	if !ok {
		context = clientcmdapi.NewContext()
	}
	if context.Extensions == nil {
		context.Extensions = map[string]runtime.Object{}
	}
	context.Extensions[constants.OktetoExtension] = nil
	context.Cluster = clusterName
	context.AuthInfo = userName
	context.Namespace = namespace
	cfg.Contexts[clusterName] = context

	cfg.CurrentContext = clusterName
}

// GetSanitizedUsername returns the username of the authenticated user sanitized to be DNS compatible
func GetSanitizedUsername() string {
	octx := Context()
	return reg.ReplaceAllString(strings.ToLower(octx.Username), "-")
}

func (okctx *OktetoContext) ToUser() *types.User {
	u := &types.User{
		ID:              okctx.UserID,
		ExternalID:      okctx.Username,
		Token:           okctx.Token,
		Buildkit:        okctx.Builder,
		Registry:        okctx.Registry,
		Certificate:     okctx.Certificate,
		GlobalNamespace: okctx.GlobalNamespace,
		Analytics:       okctx.Analytics,
	}
	return u
}

func IsOktetoCloud() bool {
	octx := Context()
	switch octx.Name {
	case constants.CloudURL, constants.StagingURL:
		return true
	default:
		return false
	}
}

func RemoveSchema(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	return strings.TrimPrefix(u.String(), fmt.Sprintf("%s://", u.Scheme))
}

func AddSchema(oCtx string) string {
	parsedUrl, err := url.Parse(oCtx)
	if err == nil {
		if parsedUrl.Scheme == "" {
			parsedUrl.Scheme = "https"
		}
		return parsedUrl.String()
	}
	return oCtx
}

func (okctx *OktetoContext) SetClusterType(clusterHost string) {
	if isLocalHostname(clusterHost) {
		okctx.ClusterType = localClusterType
	} else {
		okctx.ClusterType = remoteClusterType
	}
}

func isLocalHostname(clusterHost string) bool {
	u, err := url.Parse(clusterHost)
	host := ""
	if err == nil {
		host = u.Hostname()
		if host == "" {
			host = clusterHost
		}
	} else {
		host = clusterHost
	}

	ipAddress := net.ParseIP(host)
	return ipAddress.IsPrivate() || ipAddress.IsUnspecified() || ipAddress.IsLinkLocalUnicast() ||
		ipAddress.IsLoopback() || ipAddress.IsLinkLocalMulticast() || host == "kubernetes.docker.internal"
}

// ToViewer transforms to a viewer struct
func (c *OktetoContext) ToViewer() *OktetoContextViewer {
	builder := c.Builder
	if builder == "" {
		builder = "docker"
	}
	registry := c.Registry
	if builder == "" {
		registry = "-"
	}
	return &OktetoContextViewer{
		Name:      c.Name,
		Namespace: c.Namespace,
		Builder:   builder,
		Registry:  registry,
		Current:   Context().Name == c.Name,
	}
}

// IsOktetoContext returns if the contextName param is Okteto
func IsOktetoContext(contextName string) bool {
	ctxStore := ContextStore()
	selectedCtx, ok := ctxStore.Contexts[contextName]
	if !ok {
		return false
	}
	return selectedCtx.IsOkteto
}

func GetContextCertificate() (*x509.Certificate, error) {
	certB64 := Context().Certificate
	certPEM, err := base64.StdEncoding.DecodeString(certB64)

	if err != nil {
		oktetoLog.Debugf("couldn't decode context certificate from base64: %s", err)
		return nil, err
	}

	block, _ := pem.Decode(certPEM)

	if block == nil {
		oktetoLog.Debugf("couldn't decode context certificate from pem: %s", err)
		return nil, fmt.Errorf("couldn't decode pem")
	}

	cert, err := x509.ParseCertificate(block.Bytes)

	if err != nil {
		oktetoLog.Debugf("couldn't parse context certificate: %s", err)
		return nil, err
	}

	if _, err := cert.Verify(x509.VerifyOptions{}); err != nil { // skipcq: GO-S1031
		strictTLSOnce.Do(func() {
			oktetoLog.Debugf("certificate issuer %s", cert.Issuer)
			oktetoLog.Debugf("context certificate not trusted by system roots: %s", err)
			if cert.Issuer.CommonName == config.OktetoDefaultSelfSignedIssuer {
				hoursSinceInstall := time.Since(cert.NotBefore).Hours()
				switch {
				case hoursSinceInstall <= 24: // less than 1 day
					oktetoLog.Information("Your Okteto installation is using selfsigned certificates. Please switch to your own certificates before production use.")
				case hoursSinceInstall <= 168: // less than 1 week
					oktetoLog.Warning("Your Okteto installation has been using selfsigned certificates for more than a day. It's important to use your own certificates before production use.")
				default: // more than 1 week
					oktetoLog.Fail("[PLEASE READ] Your Okteto installation has been using selfsigned certificates for more than a week. It's important to use your own certificates before production use.")
				}
			}
		})
	}

	return cert, nil
}
