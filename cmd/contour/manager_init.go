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

package main

import (
	"context"
	"errors"

	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type NotifyReadyRunnable interface {
	manager.Runnable
	IsReady() <-chan struct{}
}

// StartManager starts a controller-runtime manager, ensuring unordered
// runnables are started and ordered runnables are started in order. Informer
// caches are synced before starting ordered runnables. Ordered runnables must
// notify they are ready before following runnables are started. Error is
// returned if adding a runnable fails or manager exits. On error, caller should
// exit immediately.
func StartManager(ctx context.Context, mgr manager.Manager, unorderedRunnables []manager.Runnable, orderedRunnables []NotifyReadyRunnable) error {
	for _, r := range unorderedRunnables {
		if err := mgr.Add(r); err != nil {
			return err
		}
	}

	errCh := make(chan error)
	go func() {
		errCh <- mgr.Start(ctx)
	}()

	if !mgr.GetCache().WaitForCacheSync(ctx) {
		return errors.New("informer cache failed to sync")
	}

	for _, r := range orderedRunnables {
		if err := mgr.Add(r); err != nil {
			return err
		}
		// If manager exits while we're waiting for a runnable to start
		// return early.
		select {
		case <-r.IsReady():
		case err := <-errCh:
			return err
		}

	}

	// Wait for manager exit.
	return <-errCh
}
