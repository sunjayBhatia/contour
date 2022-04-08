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

// Package contour contains the translation business logic that listens
// to Kubernetes ResourceEventHandler events and translates those into
// additions/deletions in caches connected to the Envoy xDS gRPC API server.
package contour

import (
	"context"
	"fmt"
	"time"

	"github.com/projectcontour/contour/internal/dag"
	"github.com/projectcontour/contour/internal/k8s"
	"github.com/sirupsen/logrus"
)

type EventHandlerConfig struct {
	Logger                        logrus.FieldLogger
	Builder                       *dag.Builder
	Observer                      dag.Observer
	HoldoffDelay, HoldoffMaxDelay time.Duration
	StatusUpdater                 k8s.StatusUpdater
}

// EventHandler implements cache.ResourceEventHandler, filters k8s events towards
// a dag.Builder and calls through to the Observer to notify it that a new DAG
// is available.
type EventHandler struct {
	builder  *dag.Builder
	observer dag.Observer

	holdoffDelay, holdoffMaxDelay time.Duration

	statusUpdater k8s.StatusUpdater

	logrus.FieldLogger

	update chan dag.CacheOp

	sequence chan int

	// seq is the sequence counter of the number of times
	// an event has been received.
	seq int
}

func NewEventHandler(config EventHandlerConfig) *EventHandler {
	return &EventHandler{
		FieldLogger:     config.Logger,
		builder:         config.Builder,
		observer:        config.Observer,
		holdoffDelay:    config.HoldoffDelay,
		holdoffMaxDelay: config.HoldoffMaxDelay,
		statusUpdater:   config.StatusUpdater,
		update:          make(chan dag.CacheOp),
		sequence:        make(chan int, 1),
	}
}

func (e *EventHandler) OnAdd(obj interface{}) {
	e.WithField("obj", fmt.Sprintf("%T", obj)).Info("OnAdd")
	e.update <- &dag.OpAdd{Obj: obj}
}

func (e *EventHandler) OnUpdate(oldObj, newObj interface{}) {
	e.WithField("oldObj", fmt.Sprintf("%T", oldObj)).WithField("newObj", fmt.Sprintf("%T", newObj)).Info("OnUpdate")
	e.update <- &dag.OpUpdate{OldObj: oldObj, NewObj: newObj}
}

func (e *EventHandler) OnDelete(obj interface{}) {
	e.WithField("obj", fmt.Sprintf("%T", obj)).Info("OnDelete")
	e.update <- &dag.OpDelete{Obj: obj}
}

func (e *EventHandler) NeedLeaderElection() bool {
	return false
}

// Implements leadership.NeedLeaderElectionNotification
func (e *EventHandler) OnElectedLeader() {
	// Trigger an update when we are elected leader to ensure resource
	// statuses are not stale.
	e.update <- &dag.OpNoop{}
}

func (e *EventHandler) Start(ctx context.Context) error {
	e.Info("started event handler")
	defer e.Info("stopped event handler")

	var (
		// outstanding counts the number of events received but not
		// yet included in a DAG rebuild.
		outstanding int

		// timer holds the timer which will expire after e.HoldoffDelay
		timer *time.Timer

		rebuildNotifyChan = make(chan dag.CacheOp)

		// lastDAGRebuild holds the last time rebuildDAG was called.
		// lastDAGRebuild is seeded to the current time on entry to
		// run to allow the holdoff timer to batch the updates from
		// the API informers.
		lastDAGRebuild = time.Now()
	)

	reset := func() (v int) {
		v, outstanding = outstanding, 0
		return
	}

	for {
		// In the main loop one of four things can happen.
		// 1. We're waiting for an event on op, stop, or pending, noting that
		//    pending may be nil if there are no pending events.
		// 2. We're processing an event.
		// 3. The holdoff timer from a previous event has fired and we're
		//    building a new DAG and sending to the Observer.
		// 4. We're stopping.
		//
		// Only one of these things can happen at a time.
		select {
		case op := <-e.update:
			if op.Apply(&e.builder.Source) {
				outstanding++
				// If there is already a timer running, stop it.
				if timer != nil {
					timer.Stop()
				}

				delay := e.holdoffDelay
				if time.Since(lastDAGRebuild) > e.holdoffMaxDelay {
					// the maximum holdoff delay has been exceeded so schedule the update
					// immediately by delaying for 0ns.
					delay = 0
				}
				timer = time.AfterFunc(delay, func() {
					rebuildNotifyChan <- op
				})
			} else {
				// notify any watchers that we received the event but chose
				// not to process it.
				e.incSequence()
			}
		case op := <-rebuildNotifyChan:
			e.WithField("last_update", time.Since(lastDAGRebuild)).WithField("outstanding", reset()).Info("performing delayed update")
			e.rebuildDAG(op)
			e.incSequence()
			lastDAGRebuild = time.Now()
		case <-ctx.Done():
			// shutdown
			return nil
		}
	}
}

// Sequence returns a channel that receives a incrementing sequence number
// for each update processed. The updates may be processed immediately, or
// delayed by a holdoff timer. In each case a non blocking send to the
// sequence channel will be made once the resource update is received (note
// that the DAG is not guaranteed to be called each time).
func (e *EventHandler) Sequence() <-chan int {
	return e.sequence
}

// incSequence bumps the sequence counter and sends it to e.Sequence.
func (e *EventHandler) incSequence() {
	e.seq++
	select {
	case e.sequence <- e.seq:
		// This is a non blocking send so if this field is nil, or the
		// receiver is not ready this send does not block incSequence's caller.
	default:
	}
}

// rebuildDAG builds a new DAG and sends it to the Observer,
// the updates the status on objects, and updates the metrics.
// passes along the change to the cace to the observer.
func (e *EventHandler) rebuildDAG(op dag.CacheOp) {
	latestDAG := e.builder.Build()
	e.observer.OnChange(latestDAG, op)

	for _, upd := range latestDAG.StatusCache.GetStatusUpdates() {
		e.statusUpdater.Send(upd)
	}
}
