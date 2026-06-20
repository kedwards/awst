package cmd

import (
	"fmt"
	"strings"
)

// authHint appends a "run awst login" suggestion to errors that look like
// credential-chain failures (missing/expired SSO token, expired temp creds,
// chain exhaustion). Action-level IAM denials are deliberately left alone
// because re-logging in doesn't fix them.
//
// ponytail: heuristic on SDK error text — there is no single sentinel type
// across the SDK's credential resolution chain. Patterns are narrow on
// purpose; a missed hint is better than a misleading one.
func authHint(err error, profile string) error {
	if err == nil {
		return nil
	}
	if !looksLikeAuthFailure(err) {
		return err
	}
	target := profile
	if target == "" {
		target = "<profile>"
	}
	return fmt.Errorf("%w\n  hint: run `awst login %s` first (then retry)", err, target)
}

var authFailureMarkers = []string{
	"no valid sso token",
	"failed to refresh cached credentials",
	"failed to retrieve credentials",
	"expiredtoken",
	"expired credentials",
	"invalidgrantexception",
}

func looksLikeAuthFailure(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, m := range authFailureMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}
