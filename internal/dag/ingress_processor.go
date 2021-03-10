// Copyright Project Contour Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dag

import (
	"strings"

	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/projectcontour/contour/internal/annotation"
	"github.com/projectcontour/contour/internal/k8s"
	"github.com/sirupsen/logrus"
	networking_v1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
)

// IngressProcessor translates Ingresses into DAG
// objects and adds them to the DAG.
type IngressProcessor struct {
	logrus.FieldLogger

	dag    *DAG
	source *KubernetesCache

	// ClientCertificate is the optional identifier of the TLS secret containing client certificate and
	// private key to be used when establishing TLS connection to upstream cluster.
	ClientCertificate *types.NamespacedName
}

// Run translates Ingresses into DAG objects and
// adds them to the DAG.
func (p *IngressProcessor) Run(dag *DAG, source *KubernetesCache) {
	p.dag = dag
	p.source = source

	// reset the processor when we're done
	defer func() {
		p.dag = nil
		p.source = nil
	}()

	p.computeIngresses()
}

// collectSecretsPerHost takes an Ingress, validates TLS settings have valid
// Secrets and certificate delegation, and returns a map of hosts to
// a further mapping of name to secret to ensure de-duplication
func (p *IngressProcessor) collectSecretsPerHost(ing *networking_v1.Ingress) map[string]*Secret {
	secretsPerHost := make(map[string]*Secret)
	for _, tls := range ing.Spec.TLS {
		secretName := k8s.NamespacedNameFrom(tls.SecretName, k8s.DefaultNamespace(ing.GetNamespace()))

		sec, err := p.source.LookupSecret(secretName, validSecret)
		if err != nil {
			p.WithError(err).
				WithField("name", ing.GetName()).
				WithField("namespace", ing.GetNamespace()).
				WithField("secret", secretName).
				Error("unresolved secret reference")
			continue
		}

		if !p.source.DelegationPermitted(secretName, ing.GetNamespace()) {
			p.WithError(err).
				WithField("name", ing.GetName()).
				WithField("namespace", ing.GetNamespace()).
				WithField("secret", secretName).
				Error("certificate delegation not permitted")
			continue
		}

		for _, host := range tls.Hosts {
			// TODO: Support multiple secrets per host. Right now the last
			// secret that references a particular host will be the only
			// one set.
			secretsPerHost[host] = sec
		}
	}
	return secretsPerHost
}

func (p *IngressProcessor) computeIngresses() {
	// deconstruct each ingress into routes and virtualhost entries
	for _, ing := range p.source.ingresses {
		secretsPerHost := p.collectSecretsPerHost(ing)
		// rewrite the default ingress to a stock ingress rule.
		rules := rulesFromSpec(ing.Spec)
		for _, rule := range rules {
			p.computeIngressRule(ing, rule, secretsPerHost)
		}
	}
}

func (p *IngressProcessor) computeIngressRule(ing *networking_v1.Ingress, rule networking_v1.IngressRule, secretsPerHost map[string]*Secret) {
	host := rule.Host
	if strings.Contains(host, "*") {
		// reject hosts with wildcard characters.
		return
	}
	if host == "" {
		// if host name is blank, rewrite to Envoy's * default host.
		host = "*"
	}

	var clientCertSecret *Secret
	var err error
	if p.ClientCertificate != nil {
		clientCertSecret, err = p.source.LookupSecret(*p.ClientCertificate, validSecret)
		if err != nil {
			p.WithError(err).
				WithField("name", ing.GetName()).
				WithField("namespace", ing.GetNamespace()).
				WithField("secret", p.ClientCertificate).
				Error("tls.envoy-client-certificate contains unresolved secret reference")
			return
		}
	}

	for _, httppath := range httppaths(rule) {
		path := stringOrDefault(httppath.Path, "/")
		be := httppath.Backend
		m := types.NamespacedName{Name: be.Service.Name, Namespace: ing.Namespace}

		var port intstr.IntOrString
		if len(be.Service.Port.Name) > 0 {
			port = intstr.FromString(be.Service.Port.Name)
		} else {
			port = intstr.FromInt(int(be.Service.Port.Number))
		}

		s, err := p.dag.EnsureService(m, port, p.source)
		if err != nil {
			p.WithError(err).
				WithField("name", ing.GetName()).
				WithField("namespace", ing.GetNamespace()).
				WithField("service", be.Service.Name).
				Error("unresolved service reference")
			continue
		}

		r, err := route(ing, path, s, clientCertSecret, p.FieldLogger)
		if err != nil {
			p.WithError(err).
				WithField("name", ing.GetName()).
				WithField("namespace", ing.GetNamespace()).
				WithField("regex", path).
				Errorf("path regex is not valid")
			return
		}

		// should we create port 80 routes for this ingress
		if annotation.TLSRequired(ing) || annotation.HTTPAllowed(ing) {
			vhost := p.dag.EnsureVirtualHost(host)
			vhost.addRoute(r)
		}

		// Do not add secure virtualhost for the "any" wildcard host.
		if host != "*" {
			if secret, found := secretsPerHost[host]; found {
				// Add secure virtualhost if there is a secret for the host
				// within this Ingress.
				svh := p.dag.EnsureSecureVirtualHost(host)
				svh.Secret = secret
				// default to a minimum TLS version of 1.2 if it's not specified
				svh.MinTLSVersion = annotation.MinTLSVersion(annotation.ContourAnnotation(ing, "tls-minimum-protocol-version"), "1.2")
				svh.addRoute(r)
			} else if svh := p.dag.GetSecureVirtualHost(host); svh != nil {
				// Allow overlaying this route on an existing secure virtualhost.
				svh.addRoute(r)
			}
		}
	}
}

// route builds a dag.Route for the supplied Ingress.
func route(ingress *networking_v1.Ingress, path string, service *Service, clientCertSecret *Secret, log logrus.FieldLogger) (*Route, error) {
	log = log.WithFields(logrus.Fields{
		"name":      ingress.Name,
		"namespace": ingress.Namespace,
	})

	r := &Route{
		HTTPSUpgrade:  annotation.TLSRequired(ingress),
		Websocket:     annotation.WebsocketRoutes(ingress)[path],
		TimeoutPolicy: ingressTimeoutPolicy(ingress, log),
		RetryPolicy:   ingressRetryPolicy(ingress, log),
		Clusters: []*Cluster{{
			Upstream:          service,
			Protocol:          service.Protocol,
			ClientCertificate: clientCertSecret,
		}},
	}

	if strings.ContainsAny(path, "^+*[]%") {
		// validate the regex
		if err := ValidateRegex(path); err != nil {
			return nil, err
		}

		r.PathMatchCondition = &RegexMatchCondition{Regex: path}
		return r, nil
	}

	r.PathMatchCondition = &PrefixMatchCondition{Prefix: path}
	return r, nil
}

// rulesFromSpec merges the IngressSpec's Rules with a synthetic rule
// representing the default backend. The default backend rule is prepended
// so subsequent rules can override it.
func rulesFromSpec(spec networking_v1.IngressSpec) []networking_v1.IngressRule {
	rules := spec.Rules
	if backend := spec.DefaultBackend; backend != nil {
		rule := defaultBackendRule(backend)
		rules = append([]networking_v1.IngressRule{rule}, rules...)
	}
	return rules
}

// defaultBackendRule returns an IngressRule that represents the IngressBackend.
func defaultBackendRule(be *networking_v1.IngressBackend) networking_v1.IngressRule {
	return networking_v1.IngressRule{
		IngressRuleValue: networking_v1.IngressRuleValue{
			HTTP: &networking_v1.HTTPIngressRuleValue{
				Paths: []networking_v1.HTTPIngressPath{{
					Backend: networking_v1.IngressBackend{
						Service: &networking_v1.IngressServiceBackend{
							Name: be.Service.Name,
							Port: be.Service.Port,
						},
					},
				}},
			},
		},
	}
}

func stringOrDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// httppaths returns a slice of HTTPIngressPath values for a given IngressRule.
// In the case that the IngressRule contains no valid HTTPIngressPaths, a
// nil slice is returned.
func httppaths(rule networking_v1.IngressRule) []networking_v1.HTTPIngressPath {
	if rule.IngressRuleValue.HTTP == nil {
		// rule.IngressRuleValue.HTTP value is optional.
		return nil
	}
	return rule.IngressRuleValue.HTTP.Paths
}
