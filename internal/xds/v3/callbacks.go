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
	"context"
	"fmt"

	envoy_service_discovery_v3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	envoy_server_v3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"github.com/sirupsen/logrus"
)

// NewRequestLoggingCallbacks returns an implementation of the Envoy xDS server
// callbacks for use when Contour is run in Envoy xDS server mode to provide
// request detail logging. Currently only the xDS State of the World callback
// OnStreamRequest is implemented.
func NewRequestLoggingCallbacks(log logrus.FieldLogger) envoy_server_v3.Callbacks {
	return &envoy_server_v3.CallbackFuncs{
		StreamResponseFunc: func(ctx context.Context, streamID int64, req *envoy_service_discovery_v3.DiscoveryRequest, resp *envoy_service_discovery_v3.DiscoveryResponse) {
			log.WithFields(logrus.Fields{"version_info": resp.VersionInfo, "nonce": resp.Nonce, "type_url": resp.TypeUrl}).Info("stream_response")
		},
		StreamRequestFunc: func(streamID int64, req *envoy_service_discovery_v3.DiscoveryRequest) error {
			logDiscoveryRequestDetails(log, req)
			return nil
		},
	}
}

// Helper function for use in the Envoy xDS server callbacks and the Contour
// xDS server to log request details. Returns logger with fields added for any
// subsequent error handling and logging.
func logDiscoveryRequestDetails(l logrus.FieldLogger, req *envoy_service_discovery_v3.DiscoveryRequest) *logrus.Entry {
	log := l.WithField("version_info", req.VersionInfo).WithField("response_nonce", req.ResponseNonce).WithField("type_url", req.TypeUrl)
	if req.Node != nil {
		log = log.WithField("node_id", req.Node.Id)

		if bv := req.Node.GetUserAgentBuildVersion(); bv != nil && bv.Version != nil {
			log = log.WithField("node_version", fmt.Sprintf("v%d.%d.%d", bv.Version.MajorNumber, bv.Version.MinorNumber, bv.Version.Patch))
		}
	}

	if status := req.ErrorDetail; status != nil {
		// if Envoy rejected the last update log the details here.
		// TODO(dfc) issue 1176: handle xDS ACK/NACK
		log.WithField("code", status.Code).WithField("details", status.Details).Error(status.Message)
	}

	log = log.WithField("resource_names", req.ResourceNames).WithField("type_url", req.GetTypeUrl())

	log.Debug("handling v3 xDS resource request")

	return log
}
