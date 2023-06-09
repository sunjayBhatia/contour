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

package v3

import (
	"testing"

	envoy_route_v3 "github.com/envoyproxy/go-control-plane/api/envoy/config/route/v3"
	envoy_discovery_v3 "github.com/envoyproxy/go-control-plane/api/envoy/service/discovery/v3"
	contour_api_v1 "github.com/projectcontour/contour/apis/projectcontour/v1"
	envoy_v3 "github.com/projectcontour/contour/internal/envoy/v3"
	"github.com/projectcontour/contour/internal/fixture"
	"github.com/projectcontour/contour/internal/ref"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestRedirectResponsePolicy_HTTProxy(t *testing.T) {
	rh, c, done := setup(t)
	defer done()

	rh.OnAdd(fixture.NewService("svc1").
		WithPorts(v1.ServicePort{Port: 80, TargetPort: intstr.FromInt(8080)}),
	)

	proxy := fixture.NewProxy("simple").WithSpec(
		contour_api_v1.HTTPProxySpec{
			VirtualHost: &contour_api_v1.VirtualHost{Fqdn: "hello.world"},
			Routes: []contour_api_v1.Route{{
				RequestRedirectPolicy: &contour_api_v1.HTTPRequestRedirectPolicy{
					Scheme:     ref.To("https"),
					Hostname:   ref.To("envoyproxy.io"),
					Port:       ref.To(int32(443)),
					StatusCode: ref.To(301),
					Path:       ref.To("/blog"),
				},
			}},
		})

	rh.OnAdd(proxy)

	c.Request(routeType).Equals(&envoy_discovery_v3.DiscoveryResponse{
		Resources: resources(t,
			envoy_v3.RouteConfiguration("ingress_http",
				envoy_v3.VirtualHost("hello.world",

					&envoy_route_v3.Route{
						Match: routePrefix("/"),
						Action: &envoy_route_v3.Route_Redirect{
							Redirect: &envoy_route_v3.RedirectAction{
								HostRedirect: "envoyproxy.io",
								PortRedirect: 443,
								SchemeRewriteSpecifier: &envoy_route_v3.RedirectAction_SchemeRedirect{
									SchemeRedirect: "https",
								},
								ResponseCode: 0,
								StripQuery:   false,
								PathRewriteSpecifier: &envoy_route_v3.RedirectAction_PathRedirect{
									PathRedirect: "/blog",
								},
							},
						},
					},
				),
			),
		),
		TypeUrl: routeType,
	})

	proxyPrefixRewrite := fixture.NewProxy("simple").WithSpec(
		contour_api_v1.HTTPProxySpec{
			VirtualHost: &contour_api_v1.VirtualHost{Fqdn: "hello.world"},
			Routes: []contour_api_v1.Route{{
				RequestRedirectPolicy: &contour_api_v1.HTTPRequestRedirectPolicy{
					Scheme:     ref.To("https"),
					Hostname:   ref.To("envoyproxy.io"),
					Port:       ref.To(int32(443)),
					StatusCode: ref.To(301),
					Prefix:     ref.To("/blogprefix"),
				},
			}},
		})

	rh.OnUpdate(proxy, proxyPrefixRewrite)

	c.Request(routeType).Equals(&envoy_discovery_v3.DiscoveryResponse{
		Resources: resources(t,
			envoy_v3.RouteConfiguration("ingress_http",
				envoy_v3.VirtualHost("hello.world",

					&envoy_route_v3.Route{
						Match: routePrefix("/"),
						Action: &envoy_route_v3.Route_Redirect{
							Redirect: &envoy_route_v3.RedirectAction{
								HostRedirect: "envoyproxy.io",
								PortRedirect: 443,
								SchemeRewriteSpecifier: &envoy_route_v3.RedirectAction_SchemeRedirect{
									SchemeRedirect: "https",
								},
								ResponseCode: 0,
								StripQuery:   false,
								PathRewriteSpecifier: &envoy_route_v3.RedirectAction_PrefixRewrite{
									PrefixRewrite: "/blogprefix",
								},
							},
						},
					},
				),
			),
		),
		TypeUrl: routeType,
	})

	proxyInvalid := fixture.NewProxy("simple").WithSpec(
		contour_api_v1.HTTPProxySpec{
			VirtualHost: &contour_api_v1.VirtualHost{Fqdn: "hello.world"},
			Routes: []contour_api_v1.Route{{
				Services: []contour_api_v1.Service{{
					Name: "svc1",
					Port: 80,
				}},
				RequestRedirectPolicy: &contour_api_v1.HTTPRequestRedirectPolicy{
					Scheme:     ref.To("https"),
					Hostname:   ref.To("envoyproxy.io"),
					Port:       ref.To(int32(443)),
					StatusCode: ref.To(301),
					Prefix:     ref.To("/blogprefix"),
					Path:       ref.To("/blogprefix"),
				},
			}},
		})

	rh.OnUpdate(proxyPrefixRewrite, proxyInvalid)

	c.Request(routeType).Equals(&envoy_discovery_v3.DiscoveryResponse{
		Resources: resources(t,
			envoy_v3.RouteConfiguration("ingress_http"),
		),
		TypeUrl: routeType,
	})
}
