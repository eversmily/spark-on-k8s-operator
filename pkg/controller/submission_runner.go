/*
Copyright 2017 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"k8s.io/spark-on-k8s-operator/pkg/apis/sparkoperator.k8s.io/v1alpha1"

	"github.com/golang/glog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// sparkSubmitRunner is responsible for running user-specified Spark applications.
type sparkSubmitRunner struct {
	workers               int
	queue                 chan *submission
	appStateReportingChan chan<- appStateUpdate
}

// appStateUpdate encapsulates overall state update of a Spark application.
type appStateUpdate struct {
	appID          string
	submissionTime metav1.Time
	completionTime metav1.Time
	state          v1alpha1.ApplicationStateType
	errorMessage   string
}

func newSparkSubmitRunner(workers int, appStateReportingChan chan<- appStateUpdate) *sparkSubmitRunner {
	return &sparkSubmitRunner{
		workers:               workers,
		queue:                 make(chan *submission, workers),
		appStateReportingChan: appStateReportingChan,
	}
}

func (r *sparkSubmitRunner) run(stopCh <-chan struct{}) {
	glog.Info("Starting the spark-submit runner")
	defer glog.Info("Stopping the spark-submit runner")

	for i := 0; i < r.workers; i++ {
		go wait.Until(r.runWorker, time.Second, stopCh)
	}

	<-stopCh
	close(r.appStateReportingChan)
}

func (r *sparkSubmitRunner) runWorker() {
	sparkHome, present := os.LookupEnv(sparkHomeEnvVar)
	if !present {
		glog.Error("SPARK_HOME is not specified")
	}
	var command = filepath.Join(sparkHome, "/bin/spark-submit")

	for s := range r.queue {
		cmd := exec.Command(command, s.args...)
		glog.Infof("spark-submit arguments: %v", cmd.Args)
		stateUpdate := appStateUpdate{
			appID:          s.appID,
			submissionTime: metav1.Now(),
		}
		if _, err := cmd.Output(); err != nil {
			stateUpdate.state = v1alpha1.FailedSubmissionState
			if exitErr, ok := err.(*exec.ExitError); ok {
				glog.Errorf("failed to submit Spark application %s: %s", s.appName, string(exitErr.Stderr))
				stateUpdate.errorMessage = string(exitErr.Stderr)
			}
		} else {
			glog.Infof("Spark application %s completed", s.appName)
			stateUpdate.state = v1alpha1.CompletedState
			stateUpdate.completionTime = metav1.Now()
		}
		// Report the application state back to the controller.
		r.appStateReportingChan <- stateUpdate
	}
}

func (r *sparkSubmitRunner) submit(s *submission) {
	r.queue <- s
}
