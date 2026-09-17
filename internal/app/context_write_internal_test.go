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

package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// TestDocumentChangedDuringChangeCarriesTheBusyCode pins the code and
// recovery writeChange gives when the document moved between ending the
// sessions a change unbinds and writing it.
func TestDocumentChangedDuringChangeCarriesTheBusyCode(t *testing.T) {
	err := documentChangedDuringChange()
	if err.Code != "contexts.document_busy" {
		t.Errorf("code = %q", err.Code)
	}
	if !strings.Contains(err.Recovery, "Retry the command") {
		t.Errorf("recovery = %q", err.Recovery)
	}
}

// TestExplainChangeRefusalRewritesOnlyTheGenericDocumentRecovery pins that a
// refusal carrying nothing but the generic "correct the document" advice is
// rewritten to say what this command left undone, while a refusal that
// already carries specific advice is returned exactly as it was.
func TestExplainChangeRefusalRewritesOnlyTheGenericDocumentRecovery(t *testing.T) {
	generic := problem.New(problem.CategoryUsage, "contexts.document_malformed", "the document is wrong").
		WithRecovery(contexts.DefaultDocumentRecovery)
	rewritten := explainChangeRefusal(generic)
	var typed problem.Problem
	if !errors.As(rewritten, &typed) {
		t.Fatalf("explainChangeRefusal did not return a problem: %v", rewritten)
	}
	if typed.Code != "shell.invalid_argument" || typed.Message != "the document is wrong" {
		t.Errorf("rewritten = %+v", typed)
	}
	if !strings.Contains(typed.Recovery, "wso2 context show") || strings.Contains(typed.Recovery, "remove it") {
		t.Errorf("the rewritten recovery does not point at context show, or still offers to remove the document: %q",
			typed.Recovery)
	}

	specific := problem.New(problem.CategoryUsage, "contexts.product_exists", "already recorded").
		WithRecovery("Pass --replace.")
	unchanged := explainChangeRefusal(specific)
	if !errors.As(unchanged, &typed) || typed.Recovery != "Pass --replace." {
		t.Errorf("a refusal with its own recovery was rewritten: %+v", typed)
	}

	plain := errors.New("not a problem at all")
	if explainChangeRefusal(plain) != plain { //nolint:errorlint // asserting reference equality on purpose
		t.Error("a non-problem error was not returned unchanged")
	}
}

// TestWriteChangeRefusesWhenTheDocumentMovedBetweenEndingSessionsAndWriting
// drives writeChange's own concurrency guard directly: the plan is built
// against one document, another write lands in between, and writeChange
// must refuse rather than clobber it, exactly the way two overlapping
// invocations of the same command would collide in production.
func TestWriteChangeRefusesWhenTheDocumentMovedBetweenEndingSessionsAndWriting(t *testing.T) {
	root := t.TempDir()
	document := contexts.Document{
		SchemaVersion:  contexts.SchemaVersion,
		DefaultContext: "acme",
		Contexts: []contexts.Context{{Name: "acme", Type: "cloud", CredentialRef: "acme",
			Login: contexts.Login{Kind: contexts.KindOAuthBrowser, Issuer: "https://idp.example", ClientID: "cli"}}},
	}
	if err := contexts.Save(root, document); err != nil {
		t.Fatal(err)
	}
	shell := Shell{StateRoot: root}

	plan, err := shell.planChange(root, func(current contexts.Document) (contexts.Document, error) {
		return current.Without("acme"), nil
	})
	if err != nil {
		t.Fatalf("planChange: %v", err)
	}

	// A second invocation runs to completion first: it adds a context the
	// plan above knows nothing about.
	err = contexts.Update(root, func(current contexts.Document) (contexts.Document, error) {
		beta := document.Contexts[0]
		beta.Name, beta.CredentialRef = "beta", "beta"
		current.Contexts = append(current.Contexts, beta)
		return current, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := shell.writeChange(root, plan); err == nil {
		t.Fatal("writeChange did not refuse a document that moved underneath it")
	} else if !strings.Contains(err.Error(), "contexts.document_busy") {
		t.Errorf("writeChange returned %v, want contexts.document_busy", err)
	}

	after, err := contexts.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := after.Find("beta"); !found {
		t.Error("the concurrent write was lost")
	}
	if _, found := after.Find("acme"); !found {
		t.Error("the refused write still removed acme")
	}
}
