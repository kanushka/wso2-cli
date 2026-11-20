// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package cobratree

import (
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

// TimeoutAnnotation is the Cobra annotation WithTimeout records a command's
// declared timeout under, in whole seconds.
const TimeoutAnnotation = "wso2.cli/timeout-seconds"

// WithTimeout declares that command needs up to limit for its answer, in
// place of the shell's default deadline, and returns command. It is for a
// command that waits on the product, such as one that follows a build until
// it finishes. limit is rounded up to a whole second; the shell holds it to a
// ceiling of its own (30 minutes). A limit of zero or less removes the
// declaration.
//
// The declaration travels in the command tree the module declares at
// installation, so it takes effect only once the module is installed again.
func WithTimeout(command *cobra.Command, limit time.Duration) *cobra.Command {
	if limit <= 0 {
		delete(command.Annotations, TimeoutAnnotation)
		return command
	}
	if command.Annotations == nil {
		command.Annotations = map[string]string{}
	}
	seconds := (limit + time.Second - 1) / time.Second
	command.Annotations[TimeoutAnnotation] = strconv.FormatInt(int64(seconds), 10)
	return command
}

// declaredTimeout reads the timeout WithTimeout recorded on command, zero when
// none was. An annotation set by other means that is not a positive whole
// number of seconds is ignored rather than declared.
func declaredTimeout(command *cobra.Command) int {
	seconds, err := strconv.Atoi(command.Annotations[TimeoutAnnotation])
	if err != nil || seconds <= 0 {
		return 0
	}
	return seconds
}
