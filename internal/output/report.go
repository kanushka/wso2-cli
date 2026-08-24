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

package output

import (
	"io"

	"github.com/wso2/wso2-cli/sdk/result"
)

// Report renders a shell-owned command's result in the given mode.
//
// It differs from Result in the table rendering only: a report is a handful of
// facts about one thing, so its labels read down the left rather than across a
// header row that would be wider than most terminals. The JSON is identical,
// and both renderings are driven by the same ordered fields, so they cannot
// disagree about what the command found. See
// docs/adr/0003-shell-owned-output.md.
//
// It takes the SDK's result type although no module is involved, because the
// shape a shell command reports and the shape a module reports are the same
// shape, and giving the shell its own would mean two renderers to keep honest.
func Report(w io.Writer, mode Mode, produced result.Result) error {
	if mode == ModeJSON {
		return resultJSON(w, produced)
	}
	pairs := make([][2]string, 0, len(produced.Fields))
	for _, field := range produced.Fields {
		pairs = append(pairs, [2]string{field.DisplayLabel(), field.Value})
	}
	return Fields(w, pairs)
}
