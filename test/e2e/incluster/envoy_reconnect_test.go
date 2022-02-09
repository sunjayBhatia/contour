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

package incluster

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	contourv1 "github.com/projectcontour/contour/apis/projectcontour/v1"
	"github.com/projectcontour/contour/test/e2e"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func testEnvoyReconnect(namespace string) {
	FSpecify("when envoy reconnects to a newly started contour, no existing configuration is lost", func() {
		// Use many proxies/apps so we have a higher likelihood of
		// Contour getting a partial set on startup.
		const numProxies = 80

		pollers := make([]*e2e.AppPoller, numProxies)

		// Just in case test fails before we get to cancel pollers so
		// they don't keep running during other tests.
		defer func() {
			for _, p := range pollers {
				if p != nil {
					p.Stop()
				}
			}
		}()

		for i := 0; i < numProxies; i++ {
			f.Fixtures.Echo.Deploy(namespace, fmt.Sprintf("echo-%d", i))

			p := &contourv1.HTTPProxy{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("reconnect-%d", i),
				},
				Spec: contourv1.HTTPProxySpec{
					VirtualHost: &contourv1.VirtualHost{
						Fqdn: fmt.Sprintf("reconnect-%d.projectcontour.io", i),
					},
					Routes: []contourv1.Route{
						{
							Services: []contourv1.Service{
								{
									Name: fmt.Sprintf("echo-%d", i),
									Port: 80,
								},
							},
						},
					},
				},
			}
			f.CreateHTTPProxyAndWaitFor(p, e2e.HTTPProxyValid)

			res, ok := f.HTTP.RequestUntil(&e2e.HTTPRequestOpts{
				Host:      p.Spec.VirtualHost.Fqdn,
				Condition: e2e.HasStatusCode(200),
			})
			require.NotNil(f.T(), res, "request never succeeded")
			require.Truef(f.T(), ok, "expected 200 response code, got %d for echo-%d", res.StatusCode, i)

			poller, err := e2e.StartAppPoller(f.HTTP.HTTPURLBase, p.Spec.VirtualHost.Fqdn, http.StatusOK)
			require.NoError(f.T(), err)
			pollers[i] = poller
		}
		By("started apps and pollers")

		// Delete deployment so Contour is effectively stopped.
		require.NoError(f.T(), f.Deployment.EnsureDeleted(f.Deployment.ContourDeployment))
		By("deleted contour deployment")

		// Wait for pods to be gone.
		require.Eventually(f.T(), func() bool {
			pods := new(v1.PodList)
			labelSelectAppContour := &client.ListOptions{
				LabelSelector: labels.SelectorFromSet(f.Deployment.ContourDeployment.Spec.Selector.MatchLabels),
				Namespace:     f.Deployment.ContourDeployment.Namespace,
			}
			if err := f.Client.List(context.TODO(), pods, labelSelectAppContour); err != nil {
				return false
			}
			if pods != nil && len(pods.Items) == 0 {
				return true
			}
			return false
		}, time.Minute, time.Millisecond*50)
		By("contour pods not running")

		env := f.Deployment.ContourDeployment.Spec.Template.Spec.Containers[0].Env
		f.Deployment.ContourDeployment.Spec.Template.Spec.Containers[0].Env = append(env,
			v1.EnvVar{
				Name:  "K8S_CLIENT_QPS",
				Value: "2",
			},
			v1.EnvVar{
				Name:  "K8S_CLIENT_BURST",
				Value: "2",
			},
		)
		require.NoError(f.T(), f.Deployment.EnsureContourDeployment())
		By("recreated contour deployment")

		require.NoError(f.T(), f.Deployment.WaitForContourDeploymentUpdated())
		By("contour deployment running")

		time.Sleep(time.Minute)
		By("waited for a minute to confirm")

		success := true
		results := make([]string, numProxies)
		for i, p := range pollers {
			p.Stop()
			total, succeeded := p.Results()
			results[i] = fmt.Sprintf("app reconnect-%d %d/%d requests", i, succeeded, total)
			if succeeded != total {
				success = false
			}
		}
		fmt.Println(strings.Join(results, ", "))
		require.True(f.T(), success, strings.Join(results, ", "))
	})
}
