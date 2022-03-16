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
	"k8s.io/utils/pointer"
)

func testNack(namespace string) {
	FSpecify("nack", func() {
		deployEchoServer(f.T(), f.Client, namespace, "echo-1")
		deployEchoServer(f.T(), f.Client, namespace, "echo-2")
		deployEchoServer(f.T(), f.Client, namespace, "echo-3")
		f.Certs.CreateSelfSignedCert(namespace, "echo-1", "echo-1", "nack.projectcontour.io")
		f.Certs.CreateSelfSignedCert(namespace, "echo-2", "echo-2", "no-nack.projectcontour.io")
		f.Certs.CreateSelfSignedCert(namespace, "echo-3", "echo-3", "no-nack-2.projectcontour.io")

		p3 := &contourv1.HTTPProxy{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "no-nack-2",
			},
			Spec: contourv1.HTTPProxySpec{
				VirtualHost: &contourv1.VirtualHost{
					Fqdn: "no-nack-2.projectcontour.io",
					// TLS: &contourv1.TLS{
					// 	SecretName: "echo-3",
					// },
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

		// res, ok := f.HTTP.SecureRequestUntil(&e2e.HTTPSRequestOpts{
		// 	Host:      p3.Spec.VirtualHost.Fqdn,
		// 	Condition: e2e.HasStatusCode(200),
		// })
		res, ok := f.HTTP.RequestUntil(&e2e.HTTPRequestOpts{
			Host:      p3.Spec.VirtualHost.Fqdn,
			Condition: e2e.HasStatusCode(200),
		})
		require.NotNil(f.T(), res, "request never succeeded")
		require.Truef(f.T(), ok, "expected 200 response code, got %d", res.StatusCode)

		p := &contourv1.HTTPProxy{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "nack",
			},
			Spec: contourv1.HTTPProxySpec{
				VirtualHost: &contourv1.VirtualHost{
					Fqdn: "nack.projectcontour.io",
					// TLS: &contourv1.TLS{
					// 	SecretName: "echo-1",
					// },
				},
				Routes: []contourv1.Route{
					{
						CookieRewritePolicies: []contourv1.CookieRewritePolicy{
							{
								Name:          "a-cookie",
								PathRewrite:   &contourv1.CookiePathRewrite{Value: "/"},
								DomainRewrite: &contourv1.CookieDomainRewrite{Value: "nack.projectcontour.io"},
								Secure:        pointer.Bool(true),
								SameSite:      pointer.String("Strict"),
							},
						},
						Services: []contourv1.Service{
							{
								Name: "echo-1",
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
								Name: "echo-1",
								Port: 80,
							},
						},
					},
				},
			},
		}
		f.CreateHTTPProxyAndWaitFor(p, e2e.HTTPProxyValid)

		// res, ok = f.HTTP.SecureRequestUntil(&e2e.HTTPSRequestOpts{
		// 	Host:      p.Spec.VirtualHost.Fqdn,
		// 	Path:      "/foo",
		// 	Condition: e2e.HasStatusCode(200),
		// })
		// require.NotNil(f.T(), res, "request never succeeded")
		// require.Truef(f.T(), ok, "expected 200 response code, got %d", res.StatusCode)

		// // Should fail
		// res, ok = f.HTTP.SecureRequestUntil(&e2e.HTTPSRequestOpts{
		// 	Host:      p.Spec.VirtualHost.Fqdn,
		// 	Path:      "/bar",
		// 	Condition: e2e.HasStatusCode(200),
		// })
		// require.NotNil(f.T(), res, "request never succeeded")
		// require.Truef(f.T(), ok, "expected 200 response code, got %d", res.StatusCode)

		p2 := &contourv1.HTTPProxy{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "no-nack",
			},
			Spec: contourv1.HTTPProxySpec{
				VirtualHost: &contourv1.VirtualHost{
					Fqdn: "no-nack.projectcontour.io",
					// TLS: &contourv1.TLS{
					// 	SecretName: "echo-2",
					// },
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

		// res, ok = f.HTTP.SecureRequestUntil(&e2e.HTTPSRequestOpts{
		// 	Host:      p2.Spec.VirtualHost.Fqdn,
		// 	Condition: e2e.HasStatusCode(200),
		// })
		res, ok = f.HTTP.RequestUntil(&e2e.HTTPRequestOpts{
			Host:      p2.Spec.VirtualHost.Fqdn,
			Condition: e2e.HasStatusCode(200),
		})
		require.NotNil(f.T(), res, "request never succeeded")
		require.Truef(f.T(), ok, "expected 200 response code, got %d", res.StatusCode)

		// make sure requests to earlier programmed resource still work
		// res, ok = f.HTTP.SecureRequestUntil(&e2e.HTTPSRequestOpts{
		// 	Host:      p3.Spec.VirtualHost.Fqdn,
		// 	Condition: e2e.HasStatusCode(200),
		// })
		res, ok = f.HTTP.RequestUntil(&e2e.HTTPRequestOpts{
			Host:      p3.Spec.VirtualHost.Fqdn,
			Condition: e2e.HasStatusCode(200),
		})
		require.NotNil(f.T(), res, "request never succeeded")
		require.Truef(f.T(), ok, "expected 200 response code, got %d", res.StatusCode)
	})
}
