package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/situker/qqmailctl/internal/account"
	"github.com/situker/qqmailctl/internal/imapx"
)

func probeS1(named account.Named, authCode string, result *result) error {
	client, greeting, err := dialRawIMAP(named)
	if err != nil {
		return err
	}
	defer client.close()
	pre, err := client.command("CAPABILITY")
	if err != nil {
		return err
	}
	login, err := client.login(authCode)
	if err != nil || !statusOK(login) {
		return fmt.Errorf("LOGIN failed: %s", client.redact(login.Text))
	}
	post, err := client.command("CAPABILITY")
	if err != nil {
		return err
	}
	bad, err := client.command("XQQMAILCTLPROBE")
	if err != nil {
		return err
	}
	examine, err := client.command("EXAMINE INBOX")
	if err != nil || !statusOK(examine) {
		return fmt.Errorf("EXAMINE failed: %s", client.redact(examine.Text))
	}
	search, err := client.command("UID SEARCH ALL")
	if err != nil {
		return err
	}
	fetchShape := []string{}
	ids := parseSearch(search.Lines)
	if len(ids) > 0 {
		fetch, fetchErr := client.command(fmt.Sprintf("UID FETCH %d (UID FLAGS RFC822.SIZE)", ids[len(ids)-1]))
		if fetchErr != nil {
			return fetchErr
		}
		fetchShape = normalizeFetch(fetch.Lines)
		result.Observations["uid_fetch_status"] = fetch.Status
	}
	preCaps := capabilities(append([]string{greeting}, pre.Lines...)...)
	postCaps := capabilities(post.Lines...)
	result.Observations["greeting_capabilities"] = capabilities(greeting)
	result.Observations["pre_login_capabilities"] = preCaps
	result.Observations["post_login_capabilities"] = postCaps
	result.Observations["feature_flags"] = map[string]bool{"MOVE": hasCapability(postCaps, "MOVE"), "UIDPLUS": hasCapability(postCaps, "UIDPLUS"), "LITERAL+": hasCapability(postCaps, "LITERAL+"), "ID": hasCapability(postCaps, "ID"), "IDLE": hasCapability(postCaps, "IDLE")}
	result.Observations["unsupported_command"] = map[string]any{"status": bad.Status, "untagged_bad": containsUntaggedBAD(bad.Lines), "response_shape": normalizeLines(bad.Lines)}
	result.Observations["uid_fetch_shape"] = fetchShape
	client.logout()
	return nil
}

func probeS2(named account.Named, authCode string, result *result) error {
	loginClient, _, err := dialRawIMAP(named)
	if err != nil {
		return err
	}
	login, err := loginClient.login(authCode)
	if err != nil {
		loginClient.close()
		return err
	}
	result.Observations["login"] = responseSummary(loginClient, login)
	if statusOK(login) {
		idReply, idErr := loginClient.command(`ID ("name" "qqmailctl" "version" "0.3.0-dev")`)
		if idErr != nil {
			loginClient.close()
			return idErr
		}
		result.Observations["id_after_login"] = responseSummary(loginClient, idReply)
	}
	loginClient.logout()

	authClient, _, err := dialRawIMAP(named)
	if err != nil {
		return err
	}
	defer authClient.close()
	authReply, err := authClient.authenticatePlain(authCode)
	if err != nil {
		return err
	}
	authReply.Text = authClient.redact(authReply.Text)
	authReply.Lines = nil
	result.Observations["authenticate_plain"] = responseSummary(authClient, authReply)
	if statusOK(authReply) {
		authClient.logout()
	}
	return nil
}

func probeS3(named account.Named, authCode string, result *result) error {
	client, _, err := dialRawIMAP(named)
	if err != nil {
		return err
	}
	defer client.close()
	login, err := client.login(authCode)
	if err != nil || !statusOK(login) {
		return fmt.Errorf("LOGIN failed: %s", client.redact(login.Text))
	}
	if reply, examineErr := client.command("EXAMINE INBOX"); examineErr != nil || !statusOK(reply) {
		return fmt.Errorf("EXAMINE failed")
	}
	matrix := map[string]any{}
	run := func(name, command string) error {
		reply, commandErr := client.command(command)
		if commandErr != nil {
			return commandErr
		}
		matrix[name] = map[string]any{"status": reply.Status, "count": len(parseSearch(reply.Lines)), "text": client.redact(reply.Text)}
		return nil
	}
	date := time.Now().AddDate(0, 0, -30).Format("02-Jan-2006")
	commands := []struct{ name, command string }{
		{"all", "UID SEARCH ALL"}, {"unseen", "UID SEARCH UNSEEN"}, {"since", "UID SEARCH SINCE " + date},
		{"subject_ascii", `UID SEARCH SUBJECT "code"`}, {"from_ascii", `UID SEARCH FROM "noreply"`},
		{"or", `UID SEARCH OR SUBJECT "code" FROM "noreply"`}, {"header", `UID SEARCH HEADER "List-Unsubscribe" ""`},
		{"utf8_subject_quoted", `UID SEARCH CHARSET UTF-8 SUBJECT "测试"`},
	}
	for _, item := range commands {
		if err := run(item.name, item.command); err != nil {
			return err
		}
	}
	literal, err := client.commandLiteral("UID SEARCH CHARSET UTF-8 SUBJECT", []byte("测试"))
	if err != nil {
		return fmt.Errorf("S3 UTF-8 literal SEARCH: %w", err)
	}
	matrix["utf8_subject_literal"] = map[string]any{"status": literal.Status, "count": len(parseSearch(literal.Lines)), "text": client.redact(literal.Text), "continuation_accepted": literal.LiteralContinuation, "result_trustworthy": literal.LiteralContinuation}
	result.Observations["search_matrix"] = matrix
	client.logout()
	return nil
}

func probeS4(named account.Named, authCode string, opts options, result *result) error {
	if !opts.write || !writeGatesOpen() {
		result.Status = "pending_manual_trigger"
		result.Pending = append(result.Pending, "Set QQMAILCTL_E2E_WRITE=1 and QQMAILCTL_DEDICATED_TEST_ACCOUNT=1, then rerun with -write. Stop at the first failure; never retry it.")
		return nil
	}
	if opts.maxLogins < 1 || opts.maxLogins > 10 {
		return fmt.Errorf("-max-logins must be 1 through 10")
	}
	attempts := make([]map[string]any, 0, opts.maxLogins)
	for i := 1; i <= opts.maxLogins; i++ {
		client, _, err := dialRawIMAP(named)
		if err != nil {
			attempts = append(attempts, map[string]any{"attempt": i, "status": "network_failure"})
			break
		}
		reply, loginErr := client.login(authCode)
		entry := map[string]any{"attempt": i, "status": reply.Status, "text": client.redact(reply.Text)}
		attempts = append(attempts, entry)
		if loginErr != nil || !statusOK(reply) {
			client.close()
			break
		}
		client.logout()
		if i < opts.maxLogins {
			time.Sleep(opts.interval)
		}
	}
	result.Observations["attempts"] = attempts
	result.Notes = append(result.Notes, "The ladder stops on the first failure and performs no automatic retry.")
	return nil
}

func probeS5(named account.Named, authCode string, opts options, result *result) error {
	values := make([]uint32, 0, 2)
	var postCaps []string
	for i := 0; i < 2; i++ {
		client, _, err := dialRawIMAP(named)
		if err != nil {
			return err
		}
		login, err := client.login(authCode)
		if err != nil || !statusOK(login) {
			client.close()
			return fmt.Errorf("LOGIN failed")
		}
		caps, err := client.command("CAPABILITY")
		if err != nil {
			client.close()
			return err
		}
		postCaps = capabilities(caps.Lines...)
		examine, err := client.command("EXAMINE INBOX")
		if err != nil {
			client.close()
			return err
		}
		values = append(values, extractUIDValidity(examine.Lines))
		client.logout()
	}
	result.Observations["uidvalidity_nonzero"] = values[0] != 0 && values[1] != 0
	result.Observations["uidvalidity_stable_across_two_sessions"] = values[0] != 0 && values[0] == values[1]
	result.Observations["uidplus_advertised"] = hasCapability(postCaps, "UIDPLUS")
	if !opts.write || !writeGatesOpen() {
		result.Status = "partially_completed"
		result.Pending = append(result.Pending, "COPY/COPYUID/UID EXPUNGE behavior requires both write gates; the read-only UIDVALIDITY phase completed.")
		return nil
	}
	return probeS5Writes(named, authCode, result)
}

func probeS5Writes(named account.Named, authCode string, result *result) error {
	client, _, err := dialRawIMAP(named)
	if err != nil {
		return err
	}
	defer client.close()
	login, err := client.login(authCode)
	if err != nil || !statusOK(login) {
		return fmt.Errorf("LOGIN failed")
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	source := "qqmailctl-probe-source-" + stamp
	target := "qqmailctl-probe-target-" + stamp
	for _, folder := range []string{source, target} {
		reply, createErr := client.command("CREATE " + quoteIMAP(folder))
		if createErr != nil || !statusOK(reply) {
			return fmt.Errorf("failed to create isolated probe folder")
		}
	}
	cleanup := func() {
		_, _ = client.command("SELECT INBOX")
		_, _ = client.command("DELETE " + quoteIMAP(source))
		_, _ = client.command("DELETE " + quoteIMAP(target))
	}
	defer cleanup()
	messageID := "<qqmailctl-probe-" + stamp + "@invalid>"
	raw := []byte("From: probe@invalid\r\nTo: probe@invalid\r\nSubject: qqmailctl S5 synthetic probe\r\nMessage-ID: " + messageID + "\r\nDate: " + time.Now().Format(time.RFC1123Z) + "\r\n\r\nSynthetic probe owned by qqmailctl.\r\n")
	appendReply, err := client.commandLiteral("APPEND "+quoteIMAP(source), raw)
	if err != nil || !statusOK(appendReply) {
		return fmt.Errorf("failed to append synthetic probe message")
	}
	selectReply, err := client.command("SELECT " + quoteIMAP(source))
	if err != nil || !statusOK(selectReply) {
		return fmt.Errorf("failed to select isolated source folder")
	}
	search, err := client.command("UID SEARCH HEADER Message-ID " + quoteIMAP(messageID))
	if err != nil {
		return err
	}
	ids := parseSearch(search.Lines)
	if len(ids) != 1 {
		return fmt.Errorf("synthetic probe message lookup returned %d UIDs", len(ids))
	}
	uid := ids[0]
	copyReply, err := client.command(fmt.Sprintf("UID COPY %d %s", uid, quoteIMAP(target)))
	if err != nil || !statusOK(copyReply) {
		return fmt.Errorf("UID COPY failed")
	}
	storeReply, err := client.command(fmt.Sprintf("UID STORE %d +FLAGS.SILENT (\\Deleted)", uid))
	if err != nil || !statusOK(storeReply) {
		return fmt.Errorf("UID STORE failed")
	}
	capsReply, _ := client.command("CAPABILITY")
	uidPlus := hasCapability(capabilities(capsReply.Lines...), "UIDPLUS")
	expunge := map[string]any{"attempted": false, "status": "skipped_without_uidplus"}
	if uidPlus {
		expungeReply, expungeErr := client.command(fmt.Sprintf("UID EXPUNGE %d", uid))
		if expungeErr != nil {
			return expungeErr
		}
		expunge = map[string]any{"attempted": true, "status": expungeReply.Status}
	}
	if reply, selectErr := client.command("EXAMINE " + quoteIMAP(target)); selectErr != nil || !statusOK(reply) {
		return fmt.Errorf("failed to examine isolated target folder")
	}
	confirm, err := client.command("UID SEARCH HEADER Message-ID " + quoteIMAP(messageID))
	if err != nil {
		return err
	}
	result.Observations["write_probe"] = map[string]any{
		"isolated_folders_created": true,
		"append_status":            appendReply.Status,
		"copy_status":              copyReply.Status,
		"copyuid_reported":         strings.Contains(strings.ToUpper(copyReply.Text), "COPYUID"),
		"target_copy_confirmed":    len(parseSearch(confirm.Lines)) == 1,
		"store_deleted_status":     storeReply.Status,
		"uid_expunge":              expunge,
		"bare_expunge_sent":        false,
	}
	client.logout()
	return nil
}

func probeS6(named account.Named, authCode string, duration time.Duration, result *result) error {
	if duration < time.Second || duration > 30*time.Minute {
		return fmt.Errorf("-duration must be between 1s and 30m")
	}
	client, _, err := dialRawIMAP(named)
	if err != nil {
		return err
	}
	defer client.close()
	login, err := client.login(authCode)
	if err != nil || !statusOK(login) {
		return fmt.Errorf("LOGIN failed")
	}
	if reply, examineErr := client.command("EXAMINE INBOX"); examineErr != nil || !statusOK(reply) {
		return fmt.Errorf("EXAMINE failed")
	}
	idleResult, err := client.idle(duration)
	if err != nil {
		return fmt.Errorf("S6 IDLE: %w", err)
	}
	result.Observations["idle"] = idleResult
	result.Notes = append(result.Notes, "A short successful observation does not prove long-term IDLE reliability; production watch remains polling-only.")
	client.logout()
	return nil
}

func probeS7(named account.Named, authCode, label string, result *result) error {
	client, _, err := dialRawIMAP(named)
	if err != nil {
		return err
	}
	defer client.close()
	login, err := client.login(authCode)
	if err != nil || !statusOK(login) {
		return fmt.Errorf("LOGIN failed")
	}
	list, err := client.command(`LIST "" "*"`)
	if err != nil {
		return err
	}
	folderCount := 0
	for _, line := range list.Lines {
		if strings.HasPrefix(strings.ToUpper(line), "* LIST ") {
			folderCount++
		}
	}
	examine, err := client.command("EXAMINE INBOX")
	if err != nil || !statusOK(examine) {
		return fmt.Errorf("EXAMINE failed")
	}
	all, err := client.command("UID SEARCH ALL")
	if err != nil {
		return err
	}
	unseen, err := client.command("UID SEARCH UNSEEN")
	if err != nil {
		return err
	}
	ids := parseSearch(all.Lines)
	result.Observations["snapshot"] = map[string]any{
		"label": label, "folder_count": folderCount, "inbox_message_count": len(ids), "inbox_unread_count": len(parseSearch(unseen.Lines)), "uidvalidity_present": extractUIDValidity(examine.Lines) != 0, "uid_set_hash": hashUIDs(ids),
	}
	result.Pending = append(result.Pending, "Change exactly one QQ web collection option, rerun with a different -label, and compare counts/hash. The probe never changes web settings.")
	result.Status = "snapshot_captured"
	client.logout()
	return nil
}

func probeS8(named account.Named, authCode string, result *result) error {
	client, _, err := dialRawIMAP(named)
	if err != nil {
		return err
	}
	defer client.close()
	login, err := client.login(authCode)
	if err != nil {
		return err
	}
	result.Observations["login_from_this_host"] = responseSummary(client, login)
	result.Status = "host_observation_only"
	result.Pending = append(result.Pending, "Run this same probe on the intended new-IP/VPS host to test provider behavior there; a local run cannot simulate another source IP.")
	if statusOK(login) {
		client.logout()
	}
	return nil
}

func probeS11(named account.Named, authCode string, opts options, result *result) error {
	if !opts.write || !writeGatesOpen() {
		result.Status = "pending_manual_trigger"
		result.Pending = append(result.Pending, "Set QQMAILCTL_E2E_WRITE=1 and QQMAILCTL_DEDICATED_TEST_ACCOUNT=1, then rerun with -write. Only uniquely named folders created by this probe are touched.")
		return nil
	}
	client, _, err := dialRawIMAP(named)
	if err != nil {
		return err
	}
	defer client.close()
	login, err := client.login(authCode)
	if err != nil || !statusOK(login) {
		return fmt.Errorf("LOGIN failed")
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	original := "qqmailctl探针-" + stamp
	renamed := original + "-已改名"
	wireOriginal := imapx.EncodeMailbox(original)
	wireRenamed := imapx.EncodeMailbox(renamed)
	create, err := client.command("CREATE " + quoteIMAP(wireOriginal))
	if err != nil || !statusOK(create) {
		return fmt.Errorf("CREATE failed")
	}
	cleanup := func() {
		_, _ = client.command("DELETE " + quoteIMAP(wireOriginal))
		_, _ = client.command("DELETE " + quoteIMAP(wireRenamed))
	}
	defer cleanup()
	rename, err := client.command("RENAME " + quoteIMAP(wireOriginal) + " " + quoteIMAP(wireRenamed))
	if err != nil || !statusOK(rename) {
		return fmt.Errorf("RENAME failed")
	}
	list, err := client.command("LIST " + quoteIMAP("") + " " + quoteIMAP(wireRenamed))
	if err != nil {
		return err
	}
	deleted, err := client.command("DELETE " + quoteIMAP(wireRenamed))
	if err != nil || !statusOK(deleted) {
		return fmt.Errorf("DELETE cleanup failed")
	}
	result.Observations["folder_probe"] = map[string]any{"create_status": create.Status, "rename_status": rename.Status, "renamed_folder_listed": countPrefix(list.Lines, "* LIST ") == 1, "cleanup_status": deleted.Status, "modified_utf7_used": wireOriginal != original}
	client.logout()
	return nil
}

func probeReadonlySuite(named account.Named, authCode string, duration time.Duration, out *result) error {
	if duration < time.Second || duration > 30*time.Minute {
		return fmt.Errorf("-duration must be between 1s and 30m")
	}
	observations := map[string]any{}
	client, greeting, err := dialRawIMAP(named)
	if err != nil {
		return err
	}
	defer client.close()
	pre, err := client.command("CAPABILITY")
	if err != nil {
		return err
	}
	login, err := client.login(authCode)
	if err != nil || !statusOK(login) {
		return fmt.Errorf("LOGIN failed: %s", client.redact(login.Text))
	}
	post, err := client.command("CAPABILITY")
	if err != nil {
		return err
	}
	postCaps := capabilities(post.Lines...)
	bad, err := client.command("XQQMAILCTLPROBE")
	if err != nil {
		return err
	}
	examine, err := client.command("EXAMINE INBOX")
	if err != nil || !statusOK(examine) {
		return fmt.Errorf("EXAMINE failed")
	}
	uidValidityOne := extractUIDValidity(examine.Lines)
	all, err := client.command("UID SEARCH ALL")
	if err != nil {
		return err
	}
	ids := parseSearch(all.Lines)
	fetchShape := []string{}
	if len(ids) > 0 {
		fetch, fetchErr := client.command(fmt.Sprintf("UID FETCH %d (UID FLAGS RFC822.SIZE)", ids[len(ids)-1]))
		if fetchErr != nil {
			return fetchErr
		}
		fetchShape = normalizeFetch(fetch.Lines)
	}
	observations["S1"] = map[string]any{
		"greeting_capabilities":   capabilities(greeting),
		"pre_login_capabilities":  capabilities(append([]string{greeting}, pre.Lines...)...),
		"post_login_capabilities": postCaps,
		"feature_flags":           map[string]bool{"MOVE": hasCapability(postCaps, "MOVE"), "UIDPLUS": hasCapability(postCaps, "UIDPLUS"), "LITERAL+": hasCapability(postCaps, "LITERAL+"), "ID": hasCapability(postCaps, "ID"), "IDLE": hasCapability(postCaps, "IDLE")},
		"unsupported_command":     map[string]any{"status": bad.Status, "untagged_bad": containsUntaggedBAD(bad.Lines), "response_shape": normalizeLines(bad.Lines)},
		"uid_fetch_shape":         fetchShape,
	}
	idReply, err := client.command(`ID ("name" "qqmailctl" "version" "0.3.0-dev")`)
	if err != nil {
		return err
	}

	matrix := map[string]any{}
	runSearch := func(name, command string) error {
		reply, searchErr := client.command(command)
		if searchErr != nil {
			return searchErr
		}
		matrix[name] = map[string]any{"status": reply.Status, "count": len(parseSearch(reply.Lines)), "text": client.redact(reply.Text)}
		return nil
	}
	date := time.Now().AddDate(0, 0, -30).Format("02-Jan-2006")
	for _, item := range []struct{ name, command string }{
		{"all", "UID SEARCH ALL"}, {"unseen", "UID SEARCH UNSEEN"}, {"since", "UID SEARCH SINCE " + date},
		{"subject_ascii", `UID SEARCH SUBJECT "code"`}, {"from_ascii", `UID SEARCH FROM "noreply"`},
		{"or", `UID SEARCH OR SUBJECT "code" FROM "noreply"`}, {"header", `UID SEARCH HEADER "List-Unsubscribe" ""`},
		{"utf8_subject_quoted", `UID SEARCH CHARSET UTF-8 SUBJECT "测试"`},
	} {
		if err := runSearch(item.name, item.command); err != nil {
			return err
		}
	}
	literal, err := client.commandLiteral("UID SEARCH CHARSET UTF-8 SUBJECT", []byte("测试"))
	if err != nil {
		return fmt.Errorf("READONLY S3 UTF-8 literal SEARCH: %w", err)
	}
	matrix["utf8_subject_literal"] = map[string]any{"status": literal.Status, "count": len(parseSearch(literal.Lines)), "text": client.redact(literal.Text), "continuation_accepted": literal.LiteralContinuation, "result_trustworthy": literal.LiteralContinuation}
	observations["S3"] = map[string]any{"search_matrix": matrix}

	list, err := client.command(`LIST "" "*"`)
	if err != nil {
		return err
	}
	unseen, err := client.command("UID SEARCH UNSEEN")
	if err != nil {
		return err
	}
	observations["S7"] = map[string]any{"snapshot": map[string]any{"label": "current", "folder_count": countPrefix(list.Lines, "* LIST "), "inbox_message_count": len(ids), "inbox_unread_count": len(parseSearch(unseen.Lines)), "uidvalidity_present": uidValidityOne != 0, "uid_set_hash": hashUIDs(ids)}}

	idleResult, err := client.idle(duration)
	if err != nil {
		return fmt.Errorf("READONLY S6 IDLE: %w", err)
	}
	observations["S6"] = map[string]any{"idle": idleResult, "production_mode": "polling_only"}
	client.logout()

	authClient, _, err := dialRawIMAP(named)
	if err != nil {
		return err
	}
	authReply, err := authClient.authenticatePlain(authCode)
	if err != nil {
		authClient.close()
		return err
	}
	authSummary := responseSummary(authClient, authReply)
	if statusOK(authReply) {
		authClient.logout()
	} else {
		authClient.close()
	}
	observations["S2"] = map[string]any{"login": responseSummary(client, login), "id_after_login": responseSummary(client, idReply), "authenticate_plain": authSummary}

	secondClient, _, err := dialRawIMAP(named)
	if err != nil {
		return err
	}
	secondLogin, err := secondClient.login(authCode)
	if err != nil || !statusOK(secondLogin) {
		secondClient.close()
		return fmt.Errorf("second LOGIN failed")
	}
	secondExamine, err := secondClient.command("EXAMINE INBOX")
	if err != nil {
		secondClient.close()
		return err
	}
	uidValidityTwo := extractUIDValidity(secondExamine.Lines)
	secondCaps, err := secondClient.command("CAPABILITY")
	if err != nil {
		secondClient.close()
		return err
	}
	secondClient.logout()
	observations["S5"] = map[string]any{"uidvalidity_nonzero": uidValidityOne != 0 && uidValidityTwo != 0, "uidvalidity_stable_across_two_sessions": uidValidityOne != 0 && uidValidityOne == uidValidityTwo, "uidplus_advertised": hasCapability(capabilities(secondCaps.Lines...), "UIDPLUS"), "write_phase": "pending_manual_trigger"}

	smtpResult := result{Observations: map[string]any{}}
	if err := probeS10(&smtpResult); err != nil {
		observations["S10"] = map[string]any{"status": "failed", "error": err.Error()}
	} else {
		observations["S10"] = smtpResult.Observations
	}
	out.Observations = observations
	out.Status = "completed_with_pending_manual_steps"
	out.Pending = append(out.Pending,
		"S5 write phase requires both write gates.",
		"S7 needs a second snapshot after one manual QQ web collection-option change.",
		"S4, S8, S9, and S11 remain separate gated/host-specific probes.",
	)
	out.Notes = append(out.Notes, "The suite reused one LOGIN session for S1/S2-ID/S3/S6/S7, then used one AUTHENTICATE session and one second LOGIN for UIDVALIDITY comparison.")
	return nil
}

func (c *rawIMAP) idle(duration time.Duration) (map[string]any, error) {
	tag := c.nextTag()
	if _, err := fmt.Fprintf(c.writer, "%s IDLE\r\n", tag); err != nil {
		return nil, err
	}
	if err := c.writer.Flush(); err != nil {
		return nil, err
	}
	_ = c.conn.SetDeadline(time.Now().Add(30 * time.Second))
	continuation, err := c.readLine()
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(continuation, "+") {
		return map[string]any{"accepted": false, "response": c.redact(continuation)}, nil
	}
	start := time.Now()
	_ = c.conn.SetReadDeadline(start.Add(duration))
	untagged := 0
	for {
		line, readErr := c.readLine()
		if readErr != nil {
			if networkErr, ok := readErr.(interface{ Timeout() bool }); ok && networkErr.Timeout() {
				break
			}
			return nil, readErr
		}
		if strings.HasPrefix(line, "*") {
			untagged++
		}
	}
	_ = c.conn.SetDeadline(time.Now().Add(30 * time.Second))
	if _, err := c.writer.WriteString("DONE\r\n"); err != nil {
		return nil, err
	}
	if err := c.writer.Flush(); err != nil {
		return nil, err
	}
	reply, err := c.readTagged(tag)
	if err != nil {
		return nil, err
	}
	return map[string]any{"accepted": true, "duration_ms": time.Since(start).Milliseconds(), "untagged_event_count": untagged, "done_status": reply.Status}, nil
}

func writeGatesOpen() bool {
	return os.Getenv("QQMAILCTL_E2E_WRITE") == "1" && os.Getenv("QQMAILCTL_DEDICATED_TEST_ACCOUNT") == "1"
}

func containsUntaggedBAD(lines []string) bool {
	for _, line := range lines {
		if strings.HasPrefix(strings.ToUpper(line), "* BAD") {
			return true
		}
	}
	return false
}

func normalizeLines(lines []string) []string {
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = numberPattern.ReplaceAllString(line, "N")
		line = emailPattern.ReplaceAllString(line, "<email>")
		result = append(result, line)
	}
	return result
}

func countPrefix(lines []string, prefix string) int {
	count := 0
	for _, line := range lines {
		if strings.HasPrefix(strings.ToUpper(line), strings.ToUpper(prefix)) {
			count++
		}
	}
	return count
}

func hashUIDs(ids []uint32) string {
	copyIDs := append([]uint32(nil), ids...)
	sort.Slice(copyIDs, func(i, j int) bool { return copyIDs[i] < copyIDs[j] })
	parts := make([]string, len(copyIDs))
	for i, id := range copyIDs {
		parts[i] = fmt.Sprint(id)
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, ",")))
	return hex.EncodeToString(digest[:])
}
