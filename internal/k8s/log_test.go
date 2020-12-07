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

package k8s

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	klog "k8s.io/klog/v2"
)

func TestKlogOnlyLogsToLogrus(t *testing.T) {
	// Save stderr/out.
	oldStderr := os.Stderr
	oldStdout := os.Stdout
	// Make sure to reset stderr/out.
	defer func() {
		os.Stderr = oldStderr
		os.Stdout = oldStdout
	}()

	// Create pipes.
	seReader, seWriter, err := os.Pipe()
	assert.NoError(t, err)
	soReader, soWriter, err := os.Pipe()
	assert.NoError(t, err)

	// Set stderr/out to pipe write end.
	os.Stderr = seWriter
	os.Stdout = soWriter

	// Channel to receive output.
	outC := make(chan string)
	go func() {
		// Close readers on exit.
		defer seReader.Close()
		defer soReader.Close()

		// Read stdout/err together.
		reader := bufio.NewReader(io.MultiReader(seReader, soReader))

		for {
			line, err := reader.ReadString('\n')
			switch err {
			case nil:
				// Send log to channel.
				outC <- line
			case io.EOF:
				// Close channel to ensure test continues.
				close(outC)
				return
			default:
				return
			}
		}
	}()

	log, logHook := test.NewNullLogger()
	l := log.WithField("foo", "bar")
	InitLogging(LogWriterOption(l))

	klog.Info("some log")
	klog.Flush()

	// Close write end of pipes.
	seWriter.Close()
	soWriter.Close()

	// Stderr/out should be empty.
	assert.Empty(t, <-outC)

	// Should be a recorded logrus log with the correct fields.
	assert.Len(t, logHook.AllEntries(), 1)
	entry := logHook.AllEntries()[0]
	assert.Equal(t, "some log\n", entry.Message)
	assert.Len(t, entry.Data, 2)
	assert.Equal(t, "bar", entry.Data["foo"])
	_, file, _, ok := runtime.Caller(0)
	assert.True(t, ok)
	// Assert this file name and some line number as location.
	assert.Regexp(t, filepath.Base(file)+":[1-9][0-9]*$", entry.Data["location"])
}

// Last LogWriterOption passed in should be used.
func TestMultipleLogWriterOptions(t *testing.T) {
	log, logHook := test.NewNullLogger()
	logEntry1 := log.WithField("field", "data1")
	logEntry2 := log.WithField("field", "data2")
	logEntry3 := log.WithField("field", "data3")
	InitLogging(LogWriterOption(logEntry1), LogWriterOption(logEntry2), LogWriterOption(logEntry3))

	klog.Info("some log")
	klog.Flush()
	assert.Len(t, logHook.AllEntries(), 1)
	assert.Equal(t, "data3", logHook.AllEntries()[0].Data["field"])
}

// test log levels

// test last log level set is used
