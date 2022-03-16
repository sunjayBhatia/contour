# xDS NACK Design

Status: Draft

## Abstract
This design outlines how Contour will handle NACK and ACK responses from Envoy.
This design will also describe what feedback users will get when resources in a Kubernetes cluster change and cause such responses from Envoy.

## Background
An aspect of Envoy's xDS protocol that Contour currently skirts around is the fact that configuration updates from Envoy can be rejected (a NACK sent in a subsequent xDS request after an xDS response from Contour) by an Envoy instance.

Currently Contour only ever programs Envoy with configuration that has been heavily validated to ensure it is not NACKed as we have no way of rolling back a change that was rejected.
We do not save and identify what resource caused a particular xDS update (and accompanying change to the DAG) and have no way of correcting the configuration we send to Envoy.
This means effectively if we get a NACK from Envoy for a particular named xDS resource (Cluster, Listener, Route, etc.), we will continue to get that forever, until the Kubernetes object containing the invalid configuration is fixed.
When a resource is NACKed, none of it's configuration is applied and we have no good way of letting the user know this (besides a log line) so debugging this situation would be very difficult.
You may have some partially applied HTTPProxy configuration and dropped traffic for example but not know why.
A particular example of this is with HTTP routes.
Contour uses a single listener for all HTTP (not HTTPS) virtualhosts and a common `RouteConfiguration` under the name `ingress_http`.
If any single route in this `RouteConfiguration` is invalid, the whole set of HTTP routes will *not* be programmed in Envoy.

As such, we have shied away from implementing features with user provided configuration values that cannot be validated in the Contour control plane before they are sent to Envoy.
This limits some of the features we can provide to users and requires us to be extremely vigilant about how we add new user configuration fields.
While this vigilance is not a bad thing per se, we do make mistakes (see [this example](https://github.com/projectcontour/contour/issues/4191)).
If we had a proper implementation of handling NACKed configuration, Contour would be more robust to real-world usage and cause as few configuration issues and drops of ingress traffic as possible.
Some examples of features that could be more confidently implemented with adequate NACK handling include custom WASM code/extension support and custom extension/filter ordering.

Similarly Contour does not actually recognize when a configuration update has been accepted (ACKed) by Envoy either.
This means Contour cannot give feedback to a user programming a relevant resource (e.g. HTTPProxy) that their configuration has been accepted and configured in the data plane.
Contour is able to set the `Valid` status condition on objects it validates as parseable/plausible configuration, but cannot set a status telling users that configuration has been applied.
Solutions built on top of Contour such as the Knative project's [net-contour](https://github.com/knative-sandbox/net-contour) actually implement a probing mechanism that repeatedly sends actual HTTP requests until a route they have programmed in Contour is in fact programmed in Envoy (and traffic can successfully flow to a backend).
While it is important to know if the configuration you have submitted is valid, it is also important to know if it has been successfully applied and it seems a better experience if Contour was able to provide this information.

## Goals
- When a kubernetes resource is created/updated and the resulting Envoy configuration update is NACKed:
  - Envoy configuration is rolled back to a previous version
  - Status and detailed error information are set on the original object
- When a kubernetes resource is created/updated and the resulting Envoy configuration update is ACKed, status on the original object is updated to indicate the change has been programmed in the data plane
- 

## Non Goals
- Setting finalizers or delaying deletion of kubernetes resources for the purpose of assessing configuration update success/failure
- 
- 

## High-Level Design
- Keep track of each k8s resource addition/deletion/update and associate it with a snapshot/xDS response
- This will allow us to react to ACKs and NACKs from Envoy and set status on the relevant object in k8s
- We currently only verify configuration and set "valid" status
- 
- Rollback when we get a NACK on a change
- When we get an ACK we can 

## Detailed Design

### Concurrent processing of k8s resource changes
For some more background on the detailed design, we should describe how changes to Kubernetes resources are processed by Contour and turn into xDS `DiscoveryResponses`

Contour sets up [informers](https://github.com/projectcontour/contour/blob/a4478d6875b0e707b0ffd4712f9dd83871210498/cmd/contour/serve.go#L461-L473) (for most resources, controllers for others) and uses the [`EventHandler`](https://github.com/projectcontour/contour/blob/a4478d6875b0e707b0ffd4712f9dd83871210498/internal/contour/handler.go) to react to resource additions, removals, and changes.
When these resource updates occur, the `EventHandler` passes them off to be asynchronously handled by its event loop.

Handling an update consists of applying it to the [`KubernetesCache`](https://github.com/projectcontour/contour/blob/a4478d6875b0e707b0ffd4712f9dd83871210498/internal/dag/cache.go), seeing if it should cause a DAG rebuild, and then rebuilding the DAG after a holdoff delay (to ensure we don't immediately run DAG rebuilds etc.).
After the DAG is rebuilt, it is sent on to the DAG observers, components that 

### What NACK responses to do programmed Envoy configuration now
- Scenario with a `RouteConfiguration` that is NACKed
- Scenario setup (in sequential order):
  - Program valid HTTPProxy
  - Program HTTPProxy that leads to configuration that is NACKed (in contrived case, added a fake feature, Lua configuration per-route, route has invalid Lua script)
    - This HTTPProxy has 2 routes, one with an invalid Lua script (that would lead to NACK) and another that does not (wouldn't be NACKed if alone)
  - Program another valid HTTPProxy
- What actually happens:
  - When all routes are set up with TLS HTTPProxies:
    - 2 valid HTTPProxies are programmed as expected, requests to their endpoints succeed even before/after all 3 resources programmed
    - For NACKed HTTPProxy, *both routes* are thrown out of Envoy config, the whole `RouteConfiguration` resource is deemed invalid since both routes are contained under a single named resource this is an issue (e.g. in test named resource `https/nack.projectcontour.io`)
  - When all routes are set up plain HTTP:
    - Only the resource programmed *before* the invalid/NACKed HTTPProxy is programmed in Envoy
    - All plain HTTP routes are organized under the `ingress_http` `RouteConfiguration` resource so any new updates are thrown out that include invalid config
    - Given we are using xDS SOTW this makes sense
    - Though would still happen with go-control-plane Delta xDS *unless* we remove the invalid config from the snapshot we generate, otherwise the delta generated by go-control-plane would contain the invalid configuration
    - This is a lot harder to debug since everything organized under one Envoy resource, unless we know what k8s resource/dag change happened to cause NACK, we don't know what route/resource to say is invalid
      - Think startup here, if Contour starts with resources that get NACKed, how do we fix that?
      - Can't expect to "find" the resource that is invalid by selectively applying changes, would drop traffic

### Ideas
- In cache, for each resource that could generate a NACK, keep copy of:
  - current generation
  - last "good" generation to roll back to
  - 
- I don't think you need this for endpoints etc. so that can simplify things the set of things that we need this mechanism for
- can build in using the correct generation of a resource into catch fetch operations
- makes it so using controller-runtime cache is harder to acheive
- event handler changes:
  - onUpdate method needs to save the update somehow so we can know what an individual update was
  - add or update
  - snapshot handler needs to have some version coordination with xds server

### Expected UX
- when a k8s resource is updated with config that is NACKed, user is notified
  - status set on objects that have it
  - Contour logs with which resource is nacked, error details if possible
  - possibly add detail to an annotation for objects w/o status (Ingress)
- Contour will keep the Envoy configuration for the previous generation of that object (or none if it is a new object)
- updates to *other* k8s resources are applied to Envoy configuration
- when resource is "fixed" Envoy configuration is updated
- If Contour starts up and there is an object who generates a NACKed configuration, it will exclude that object (treat it as a new object)
  - *This case is a little interesting, since we can't fetch an older generation of an object unless we cached it*
- Logic demonstrated by e2e tests
  - Don't have to commit them in final implementation
  - Can be replaced by lower level unit tests
  - Written for demonstration/agreement from reviewers purposes mostly, sort of like conformance

### General Flow
- We need to keep track of each "operation" seen by the event handler and record if it is NACKed or ACKed
  - Resources being added
  - Resources being removed
  - Resources being updated (we have the before and after)
  - Right now operations are used to modify cache, which calculates whether we need a dag rebuild and then dropped
- Associate each operation with a snapshot and save it since a snapshot is generated for each resource change that is relevant to the dag
  - e.g. pseudocode: map[Version]Operation
  - Added to on each dag cache change/snapshot generation
  - *What do we do on startup*
- When we get a NACK
  - xDS Request will include nonce of previous xDS Response the NACK is for
  - also includes last "good" resource/snapshot version
  - roll back 
- When we get an ACK, we can remove the operation

### What does Istio do
- don't actually roll back changes as far as I can tell
- just log and optionally push status info about NACK/ACK to subscribers (k8s resource status updates)
- do not respond to DiscoveryRequest with error details
- https://istio.io/latest/docs/reference/config/config-status/
- xDS server event loop combines handling "pushes" from resource changes and responding to DiscoveryRequests in same thread
- xDS config is re-generated for a particular resource type only if the "pushed" change was relevant to that type
  - e.g. if a secret changes, don't need to re-gen Endpoints

### Multiple Envoy instances sending ACKs and NACKs
- How do we deal with Envoys sending different ACKs/NACKs for a change?
- Is this even theoretically possible? (between versions of Envoy?)

### Restart problem
- revert to single Contour active at a time serving xDS?
- e.g. esp. if a single Contour instance restarts it doesn't know how to roll back a resource change
- we may implement this as having to reject the whole resource
- so you might get some Envoys with a proper rollback and some with more rolled back than required
- that inconsistency seems worse than a more uniform (though possibly larger) rollback

### Multiple resource changes before we process a NACK
- Resource X changes w/ invalid config and snapshot version N generated, sent to clients (will be NACKed)
- *Because snapshot generation and ACK/NACK processing is concurrent:* before we process any further requests resource Y changes also w/ invalid config, and snapshot version N+1 generated
- Snapshot version N+1 contains resource X's latest version and resource Y's latest version (will be NACKed for 2 reasons)
- Resource Z changes w/ invalid config and snapshot version N+2 generated
- Snapshot version N+2 contains resource X's latest version and resource Y's latest version  (will be NACKed)
- When we get a NACK for e.g. the Listener stream for snapshot N+2, we don't actually know at this point what resource caused the NACK, it sort of "looks like" it was resource Z
- If e.g. a dag build is slow, reverting any of these changes could take a while, and resource changes/new snapshots will stack up 

## Alternatives Considered

### Repurpose Valid Condition to indicate configuration has been programmed
Rather than adding a new status condition, we could instead delay setting the `Valid` status condition until a configuration update is ACKed or NACKed.
This option was not chosen as it overloads ***********

### Roll back implementation removes whole resource that caused NACK
When we get a NACK from Envoy, instead of rolling back to a previous version of an object that caused a NACK, we would totally disregard it when building the DAG.
This has a benefit of making it easier with multiple Contour instances if there were any instances restarted while invalid resources existed in the cluster.
We would not have any inconsistencies between what different Contour instances knew about.
However, this does have the disadvantage of undoing possibly valid traffic and dropping traffic.

### On NACK, roll back to previous version of k8s resource
- doesnt drop traffic
- however each contour instance needs to know before/after
- can't really have multiple active Contours as xDS servers
- on startup, a new contour instance will just see a resource as an addition, so will then ignore it if it is NACKed, not roll back

## Security Considerations
If this proposal has an impact to the security of the product, its users, or data stored or transmitted via the product, they must be addressed here.

## Compatibility
A discussion of any compatibility issues that need to be considered

## Implementation
A description of the implementation, timelines, and any resources that have agreed to contribute.

## Open Issues
A discussion of issues relating to this proposal for which the author does not know the solution. This section may be omitted if there are none.
