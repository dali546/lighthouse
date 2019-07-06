/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package invalidcommitmsg

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/jenkins-x/go-scm/scm"
	"github.com/sirupsen/logrus"

	"github.com/jenkins-x/lighthouse/pkg/prow/fakegithub"
)

type fakePruner struct{}

func (fp *fakePruner) PruneComments(shouldPrune func(scm.Comment) bool) {}

func strP(str string) *string {
	return &str
}

func makeFakePullRequestEvent(action scm.Action) scm.PullRequestHook {
	return scm.PullRequestHook{
		Action: action,
		PullRequest: scm.PullRequest{
			Number: 3,
		},
		Repo: scm.Repository{
			Namespace: "k",
			Name:      "k",
		},
	}
}

func TestHandlePullRequest(t *testing.T) {
	var testcases = []struct {
		name string

		// PR settings
		action                       scm.Action
		commits                      []scm.Commit
		hasInvalidCommitMessageLabel bool

		// expectations
		addedLabel   string
		removedLabel string
		addedComment string
	}{
		{
			name:   "unsupported PR action -> no-op",
			action: scm.ActionEdited,
		},
		{
			name:   "contains valid message -> no-op",
			action: scm.ActionOpen,
			commits: []scm.Commit{
				{Sha: "sha1", Message: "this is a valid message"},
				{Sha: "sha2", Message: "fixing k/k#9999"},
				{Sha: "sha3", Message: "not a @ mention"},
			},
			hasInvalidCommitMessageLabel: false,
		},
		{
			name:   "msg contains invalid keywords -> add label and comment",
			action: scm.ActionOpen,
			commits: []scm.Commit{
				{Sha: "sha1", Message: "this is a @mention"},
				{Sha: "sha2", Message: "this @menti-on has a hyphen"},
				{Sha: "sha3", Message: "this @Menti-On has mixed case letters"},
				{Sha: "sha4", Message: "fixes k/k#9999"},
				{Sha: "sha5", Message: "Close k/k#9999"},
				{Sha: "sha6", Message: "resolved k/k#9999"},
				{Sha: "sha7", Message: "this is an email@address and is valid"},
			},
			hasInvalidCommitMessageLabel: false,

			addedLabel: fmt.Sprintf("k/k#3:%s", invalidCommitMsgLabel),
			addedComment: `k/k#3:[Keywords](https://help.github.com/articles/closing-issues-using-keywords) which can automatically close issues and at(@) mentions are not allowed in commit messages.

**The list of commits with invalid commit messages**:

- [sha1](https://github.com/k/k/commits/sha1) this is a @mention
- [sha2](https://github.com/k/k/commits/sha2) this @menti-on has a hyphen
- [sha3](https://github.com/k/k/commits/sha3) this @Menti-On has mixed case letters
- [sha4](https://github.com/k/k/commits/sha4) fixes k/k#9999
- [sha5](https://github.com/k/k/commits/sha5) Close k/k#9999
- [sha6](https://github.com/k/k/commits/sha6) resolved k/k#9999

<details>

Instructions for interacting with me using PR comments are available [here](https://git.k8s.io/community/contributors/guide/pull-requests.md).  If you have questions or suggestions related to my behavior, please file an issue against the [kubernetes/test-infra](https://github.com/kubernetes/test-infra/issues/new?title=Prow%20issue:) repository. I understand the commands that are listed [here](https://go.k8s.io/bot-commands).
</details>
`,
		},
		{
			name:   "msg does not contain invalid keywords but has label -> remove label",
			action: scm.ActionOpen,
			commits: []scm.Commit{
				{Sha: "sha", Message: "this is a valid message"},
			},
			hasInvalidCommitMessageLabel: true,

			removedLabel: fmt.Sprintf("k/k#3:%s", invalidCommitMsgLabel),
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			event := makeFakePullRequestEvent(tc.action)
			fc := &fakegithub.FakeClient{
				PullRequests:  map[int]*scm.PullRequest{event.PullRequest.Number: &event.PullRequest},
				IssueComments: make(map[int][]scm.Comment),
				CommitMap: map[string][]scm.Commit{
					"k/k#3": tc.commits,
				},
			}

			if tc.hasInvalidCommitMessageLabel {
				fc.IssueLabelsAdded = append(fc.IssueLabelsAdded, fmt.Sprintf("k/k#3:%s", invalidCommitMsgLabel))
			}
			if err := handle(fc, logrus.WithField("plugin", pluginName), event, &fakePruner{}); err != nil {
				t.Errorf("For case %s, didn't expect error from invalidcommitmsg plugin: %v", tc.name, err)
			}

			ok := tc.addedLabel == ""
			if !ok {
				for _, label := range fc.IssueLabelsAdded {
					if reflect.DeepEqual(tc.addedLabel, label) {
						ok = true
						break
					}
				}
			}
			if !ok {
				t.Errorf("Expected to add: %#v, Got %#v in case %s.", tc.addedLabel, fc.IssueLabelsAdded, tc.name)
			}

			ok = tc.removedLabel == ""
			if !ok {
				for _, label := range fc.IssueLabelsRemoved {
					if reflect.DeepEqual(tc.removedLabel, label) {
						ok = true
						break
					}
				}
			}
			if !ok {
				t.Errorf("Expected to remove: %#v, Got %#v in case %s.", tc.removedLabel, fc.IssueLabelsRemoved, tc.name)
			}

			comments := fc.IssueCommentsAdded
			if len(comments) == 0 && tc.addedComment != "" {
				t.Errorf("Expected comment with body %q to be added, but it was not", tc.addedComment)
				return
			}
			if len(comments) > 1 {
				t.Errorf("did not expect more than one comment to be created")
			}
			if len(comments) != 0 && comments[0] != tc.addedComment {
				t.Errorf("expected comment to be \n%q\n but it was \n%q\n", tc.addedComment, comments[0])
			}
		})
	}
}
