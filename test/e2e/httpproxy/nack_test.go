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

//go:build e2e
// +build e2e

package httpproxy

import (
	. "github.com/onsi/ginkgo/v2"
	contourv1 "github.com/projectcontour/contour/apis/projectcontour/v1"
	"github.com/projectcontour/contour/test/e2e"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func testNACKHTTPRoute(namespace string) {
	Specify("NACK http route", func() {
		f.Fixtures.Echo.Deploy(namespace, "echo-1")
		f.Fixtures.Echo.Deploy(namespace, "echo-2")
		f.Fixtures.Echo.Deploy(namespace, "echo-3")

		p1 := &contourv1.HTTPProxy{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "no-nack",
			},
			Spec: contourv1.HTTPProxySpec{
				VirtualHost: &contourv1.VirtualHost{
					Fqdn: "no-nack-http.projectcontour.io",
				},
				Routes: []contourv1.Route{
					{
						Services: []contourv1.Service{
							{
								Name: "echo-1",
								Port: 80,
							},
						},
					},
				},
			},
		}
		f.CreateHTTPProxyAndWaitFor(p1, e2e.HTTPProxyValid)
		res, ok := f.HTTP.RequestUntil(&e2e.HTTPRequestOpts{
			Host:      p1.Spec.VirtualHost.Fqdn,
			Condition: e2e.HasStatusCode(200),
		})
		require.NotNil(f.T(), res, "request never succeeded")
		require.Truef(f.T(), ok, "expected 200 response code, got %d", res.StatusCode)

		p2 := &contourv1.HTTPProxy{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "nack",
			},
			Spec: contourv1.HTTPProxySpec{
				VirtualHost: &contourv1.VirtualHost{
					Fqdn: "nack-http.projectcontour.io",
				},
				Routes: []contourv1.Route{
					{
						LuaScript: "invalid",
						Services: []contourv1.Service{
							{
								Name: "echo-2",
								Port: 80,
							},
						},
					},
					{
						Conditions: []contourv1.MatchCondition{
							{
								Prefix: "/foo",
							},
						},
						Services: []contourv1.Service{
							{
								Name: "echo-2",
								Port: 80,
							},
						},
					},
				},
			},
		}
		f.CreateHTTPProxyAndWaitFor(p2, e2e.HTTPProxyValid)
		// TODO: this should eventually be a status that the proxy was invalid
		// Also test that requests get 404s

		p3 := &contourv1.HTTPProxy{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "no-nack-2",
			},
			Spec: contourv1.HTTPProxySpec{
				VirtualHost: &contourv1.VirtualHost{
					Fqdn: "no-nack-http-2.projectcontour.io",
				},
				Routes: []contourv1.Route{
					{
						Services: []contourv1.Service{
							{
								Name: "echo-3",
								Port: 80,
							},
						},
					},
				},
			},
		}
		f.CreateHTTPProxyAndWaitFor(p3, e2e.HTTPProxyValid)

		res, ok = f.HTTP.RequestUntil(&e2e.HTTPRequestOpts{
			Host:      p3.Spec.VirtualHost.Fqdn,
			Condition: e2e.HasStatusCode(200),
		})
		require.NotNil(f.T(), res, "request never succeeded")
		require.Truef(f.T(), ok, "expected 200 response code, got %d", res.StatusCode)

		// make sure requests to earlier programmed resource still work
		res, ok = f.HTTP.RequestUntil(&e2e.HTTPRequestOpts{
			Host:      p1.Spec.VirtualHost.Fqdn,
			Condition: e2e.HasStatusCode(200),
		})
		require.NotNil(f.T(), res, "request never succeeded")
		require.Truef(f.T(), ok, "expected 200 response code, got %d", res.StatusCode)
	})
}

func testNACKHTTPSRoute(namespace string) {
	FSpecify("NACK https route", func() {
		f.Fixtures.Echo.Deploy(namespace, "echo-1")
		f.Fixtures.Echo.Deploy(namespace, "echo-2")
		f.Fixtures.Echo.Deploy(namespace, "echo-3")
		f.Certs.CreateSelfSignedCert(namespace, "echo-1", "echo-1", "nack-https.projectcontour.io")
		f.Certs.CreateSelfSignedCert(namespace, "echo-2", "echo-2", "no-nack-https.projectcontour.io")
		f.Certs.CreateSelfSignedCert(namespace, "echo-3", "echo-3", "no-nack-https-2.projectcontour.io")

		p1 := &contourv1.HTTPProxy{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "no-nack",
			},
			Spec: contourv1.HTTPProxySpec{
				VirtualHost: &contourv1.VirtualHost{
					Fqdn: "no-nack-https.projectcontour.io",
					TLS: &contourv1.TLS{
						SecretName: "echo-1",
					},
				},
				Routes: []contourv1.Route{
					{
						Services: []contourv1.Service{
							{
								Name: "echo-1",
								Port: 80,
							},
						},
					},
				},
			},
		}
		f.CreateHTTPProxyAndWaitFor(p1, e2e.HTTPProxyValid)
		res, ok := f.HTTP.SecureRequestUntil(&e2e.HTTPSRequestOpts{
			Host:      p1.Spec.VirtualHost.Fqdn,
			Condition: e2e.HasStatusCode(200),
		})
		require.NotNil(f.T(), res, "request never succeeded")
		require.Truef(f.T(), ok, "expected 200 response code, got %d", res.StatusCode)

		p2 := &contourv1.HTTPProxy{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "nack",
			},
			Spec: contourv1.HTTPProxySpec{
				VirtualHost: &contourv1.VirtualHost{
					Fqdn: "nack-https.projectcontour.io",
					TLS: &contourv1.TLS{
						SecretName: "echo-2",
					},
				},
				Routes: []contourv1.Route{
					{
						LuaScript: "invalid",
						Services: []contourv1.Service{
							{
								Name: "echo-2",
								Port: 80,
							},
						},
					},
					{
						Conditions: []contourv1.MatchCondition{
							{
								Prefix: "/foo",
							},
						},
						Services: []contourv1.Service{
							{
								Name: "echo-2",
								Port: 80,
							},
						},
					},
				},
			},
		}
		f.CreateHTTPProxyAndWaitFor(p2, e2e.HTTPProxyValid)
		// TODO: this should eventually be a status that the proxy was invalid
		// Also test that requests get 404s

		p3 := &contourv1.HTTPProxy{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "no-nack-2",
			},
			Spec: contourv1.HTTPProxySpec{
				VirtualHost: &contourv1.VirtualHost{
					Fqdn: "no-nack-https-2.projectcontour.io",
					TLS: &contourv1.TLS{
						SecretName: "echo-3",
					},
				},
				Routes: []contourv1.Route{
					{
						Services: []contourv1.Service{
							{
								Name: "echo-3",
								Port: 80,
							},
						},
					},
				},
			},
		}
		f.CreateHTTPProxyAndWaitFor(p3, e2e.HTTPProxyValid)

		res, ok = f.HTTP.SecureRequestUntil(&e2e.HTTPSRequestOpts{
			Host:      p3.Spec.VirtualHost.Fqdn,
			Condition: e2e.HasStatusCode(200),
		})
		require.NotNil(f.T(), res, "request never succeeded")
		require.Truef(f.T(), ok, "expected 200 response code, got %d", res.StatusCode)

		// make sure requests to earlier programmed resource still work
		res, ok = f.HTTP.SecureRequestUntil(&e2e.HTTPSRequestOpts{
			Host:      p1.Spec.VirtualHost.Fqdn,
			Condition: e2e.HasStatusCode(200),
		})
		require.NotNil(f.T(), res, "request never succeeded")
		require.Truef(f.T(), ok, "expected 200 response code, got %d", res.StatusCode)
	})
}

func testNACKListener(namespace string) {
	Specify("NACK on a listener level", func() {
		f.Fixtures.Echo.Deploy(namespace, "echo-1")
		f.Fixtures.Echo.Deploy(namespace, "echo-2")

		p1 := &contourv1.HTTPProxy{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "nack-listener",
			},
			Spec: contourv1.HTTPProxySpec{
				VirtualHost: &contourv1.VirtualHost{
					Fqdn: "nack-listener.projectcontour.io",
				},
				Routes: []contourv1.Route{
					{
						Services: []contourv1.Service{
							{
								Name: "echo-1",
								Port: 80,
							},
						},
					},
				},
			},
		}
		f.CreateHTTPProxyAndWaitFor(p1, e2e.HTTPProxyValid)
		// TODO: this should eventually be a status that the proxy was invalid
		// Also test that requests get 404s

		p2 := &contourv1.HTTPProxy{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "no-nack-listener",
			},
			Spec: contourv1.HTTPProxySpec{
				VirtualHost: &contourv1.VirtualHost{
					Fqdn: "no-nack-listener.projectcontour.io",
				},
				Routes: []contourv1.Route{
					{
						Services: []contourv1.Service{
							{
								Name: "echo-2",
								Port: 80,
							},
						},
					},
				},
			},
		}
		f.CreateHTTPProxyAndWaitFor(p2, e2e.HTTPProxyValid)

		// Requests to this app should still work.
		res, ok := f.HTTP.SecureRequestUntil(&e2e.HTTPSRequestOpts{
			Host:      p2.Spec.VirtualHost.Fqdn,
			Condition: e2e.HasStatusCode(200),
		})
		require.NotNil(f.T(), res, "request never succeeded")
		require.Truef(f.T(), ok, "expected 200 response code, got %d", res.StatusCode)
	})
}
