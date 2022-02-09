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
	"testing"
	"time"

	"github.com/projectcontour/contour/cmd/contour/mocks"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

//go:generate go run github.com/vektra/mockery/v2 --case=snake --name=Manager --srcpkg=sigs.k8s.io/controller-runtime/pkg/manager
//go:generate go run github.com/vektra/mockery/v2 --case=snake --name=Runnable --srcpkg=sigs.k8s.io/controller-runtime/pkg/manager
//go:generate go run github.com/vektra/mockery/v2 --case=snake --name=NotifyReadyRunnable

func TestStartManager(t *testing.T) {
	testCases := map[string]struct {
		assertions func(*mocks.Manager, [2]manager.Runnable, [2]NotifyReadyRunnable) error
		startExits bool
	}{
		"simple": {
			assertions: func(mgr *mocks.Manager, unordered [2]manager.Runnable, ordered [2]NotifyReadyRunnable) error {
				for _, r := range unordered {
					mgr.On("Add", r).Return(nil).Once()
				}

				for _, r := range ordered {
					mgr.On("Add", r).Return(nil).Once()
				}

				return nil
			},
		},
		// "error adding unordered": {
		// 	assertions: func(mgr *mocks.Manager, unordered [2]manager.Runnable, ordered [2]NotifyReadyRunnable) error {
		// 		mgr.On("Add", unordered[0]).Return(nil).Once()
		// 		addErr := errors.New("failed adding unordered")
		// 		mgr.On("Add", unordered[1]).Return(addErr).Once()

		// 		return addErr
		// 	},
		// 	startExits: true,
		// },
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			mockManager := new(mocks.Manager)
			unordered := [2]manager.Runnable{new(mocks.Runnable), new(mocks.Runnable)}
			ordered := [2]NotifyReadyRunnable{new(mocks.NotifyReadyRunnable), new(mocks.NotifyReadyRunnable)}

			expectedErr := tc.assertions(mockManager, unordered, ordered)

			ctx := context.TODO()
			stopCh := make(chan time.Time)
			mockManager.On("Start", ctx).WaitUntil(stopCh).Return(nil).Once()
			errCh := make(chan error)
			go func() {
				errCh <- StartManager(ctx, mockManager, unordered[:], ordered[:])
			}()

			var exitErr error
			if tc.startExits {
				require.Eventually(t, func() bool {
					select {
					case exitErr = <-errCh:
						return true
					default:
						return false
					}
				}, time.Second, time.Millisecond*10)
			} else {
				time.Sleep(time.Second * 1)
				close(stopCh)
			}

			if expectedErr != nil {
				require.ErrorIs(t, exitErr, expectedErr)
			} else {
				require.NoError(t, exitErr)
			}

			require.True(t, mockManager.AssertExpectations(t))
		})
	}
}
