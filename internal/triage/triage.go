package triage

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/situker/qqmailctl/internal/cleanupplan"
	"github.com/situker/qqmailctl/internal/index"
	"github.com/situker/qqmailctl/internal/mailmodel"
)

type RuleFile struct {
	Rules []Rule `toml:"rules"`
}

type Rule struct {
	Name       string  `toml:"name"`
	Header     string  `toml:"header"`
	From       string  `toml:"from"`
	Subject    string  `toml:"subject"`
	Category   string  `toml:"category"`
	Confidence float64 `toml:"confidence"`
	Reason     string  `toml:"reason"`

	headerRE  *regexp.Regexp
	fromRE    *regexp.Regexp
	subjectRE *regexp.Regexp
}

type Bucket struct {
	Count       int     `json:"count"`
	SizeBytes   int64   `json:"size_bytes"`
	UnreadCount int     `json:"unread_count"`
	UnreadRate  float64 `json:"unread_rate"`
}

type Analysis struct {
	TotalCount     int               `json:"total_count"`
	TotalSizeBytes int64             `json:"total_size_bytes"`
	ByCategory     map[string]Bucket `json:"by_category"`
	ByFromDomain   map[string]Bucket `json:"by_from_domain"`
	ByAgeBucket    map[string]Bucket `json:"by_age_bucket"`
	BySizeBucket   map[string]Bucket `json:"by_size_bucket"`
	CustomRules    int               `json:"custom_rules"`
	PlanEligible   int               `json:"plan_eligible"`
	ExcludedByRule map[string]int    `json:"excluded_by_rule"`
}

// DefaultCleanupCategories is the closed set of categories a plan may target
// unless the operator explicitly widens it. "keep", "verification", "receipt",
// "other" and every custom category stay out of cleanup plans by default so a
// generated plan is never an "empty the whole mailbox" instruction.
var DefaultCleanupCategories = []string{"marketing", "machine_notification", "social_notification"}

// Options bounds which classified messages may enter a cleanup plan. The zero
// value is intentionally useless: callers must go through NewOptions so every
// safety default is applied.
type Options struct {
	Categories    map[string]bool
	MinConfidence float64
	OlderThan     time.Time // zero disables the age floor
	Folders       map[string]bool
}

func NewOptions(include, exclude []string, minConfidence float64, olderThan time.Time, folders []string) Options {
	categories := map[string]bool{}
	for _, category := range DefaultCleanupCategories {
		categories[normalizeToken(category)] = true
	}
	for _, category := range include {
		if token := normalizeToken(category); token != "" {
			categories[token] = true
		}
	}
	for _, category := range exclude {
		delete(categories, normalizeToken(category))
	}
	folderSet := map[string]bool{}
	for _, folder := range folders {
		if token := normalizeToken(folder); token != "" {
			folderSet[token] = true
		}
	}
	return Options{Categories: categories, MinConfidence: minConfidence, OlderThan: olderThan, Folders: folderSet}
}

func normalizeToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

var (
	noreplyPattern      = regexp.MustCompile(`(?i)(^|[._+-])(no-?reply|do-?not-?reply|notification|mailer-daemon)([._+@-]|$)`)
	verificationPattern = regexp.MustCompile(`(?i)(验证码|校验码|动态码|一次性密码|verification\s*code|security\s*code|otp)`)
)

var socialDomains = map[string]bool{"linkedin.com": true, "facebookmail.com": true, "weibo.com": true, "zhihu.com": true}
var receiptDomains = map[string]bool{"alipay.com": true, "paypal.com": true, "stripe.com": true, "2checkout.com": true}

func ParseRules(raw []byte) ([]Rule, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var file RuleFile
	decoder := toml.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, err
	}
	for i := range file.Rules {
		rule := &file.Rules[i]
		if strings.TrimSpace(rule.Category) == "" || strings.TrimSpace(rule.Reason) == "" {
			return nil, fmt.Errorf("rules[%d] requires category and reason", i)
		}
		if rule.Header == "" && rule.From == "" && rule.Subject == "" {
			return nil, fmt.Errorf("rules[%d] requires at least one regex", i)
		}
		if rule.Confidence <= 0 || rule.Confidence > 1 {
			return nil, fmt.Errorf("rules[%d] confidence must be greater than 0 and at most 1", i)
		}
		var err error
		if rule.headerRE, err = compileBounded(rule.Header); err != nil {
			return nil, fmt.Errorf("rules[%d].header: %w", i, err)
		}
		if rule.fromRE, err = compileBounded(rule.From); err != nil {
			return nil, fmt.Errorf("rules[%d].from: %w", i, err)
		}
		if rule.subjectRE, err = compileBounded(rule.Subject); err != nil {
			return nil, fmt.Errorf("rules[%d].subject: %w", i, err)
		}
	}
	return file.Rules, nil
}

func Build(messages []index.Message, rules []Rule, now time.Time, opts Options) (cleanupplan.Plan, Analysis) {
	plan := cleanupplan.Plan{Schema: cleanupplan.Schema, CreatedAt: now.UTC(), Items: []cleanupplan.Item{}, Statistics: cleanupplan.Statistics{ByCategory: map[string]int{}, ByFromDomain: map[string]int{}}}
	analysis := Analysis{ByCategory: map[string]Bucket{}, ByFromDomain: map[string]Bucket{}, ByAgeBucket: map[string]Bucket{}, BySizeBucket: map[string]Bucket{}, CustomRules: len(rules), ExcludedByRule: map[string]int{}}
	for _, message := range messages {
		category, confidence, reason, evidence := classify(message, rules)
		date := message.DateHeader
		if date.IsZero() {
			date = message.InternalDate
		}
		if date.IsZero() {
			date = time.Unix(0, 0).UTC()
		}
		domain := fromDomain(message.FromAddr)
		if domain == "" {
			domain = "(unknown)"
		}
		unread := !hasFlag(message.Flags, `\Seen`)
		addBucket(analysis.ByCategory, category, message.SizeBytes, unread)
		addBucket(analysis.ByFromDomain, domain, message.SizeBytes, unread)
		addBucket(analysis.ByAgeBucket, ageBucket(now, message.InternalDate), message.SizeBytes, unread)
		addBucket(analysis.BySizeBucket, sizeBucket(message.SizeBytes), message.SizeBytes, unread)
		analysis.TotalCount++
		analysis.TotalSizeBytes += message.SizeBytes
		if excluded := excludeReason(message, category, confidence, date, now, opts); excluded != "" {
			analysis.ExcludedByRule[excluded]++
			continue
		}
		from := []mailmodel.Address{}
		if message.FromAddr != "" || message.FromName != "" {
			from = append(from, mailmodel.Address{Name: message.FromName, Email: message.FromAddr})
		}
		plan.Items = append(plan.Items, cleanupplan.Item{ID: message.ID, Category: category, Confidence: confidence, Reason: reason, Evidence: evidence, From: from, Subject: message.Subject, Date: date, SizeBytes: message.SizeBytes})
		plan.Statistics.TotalCount++
		plan.Statistics.TotalSizeBytes += message.SizeBytes
		plan.Statistics.ByCategory[category]++
		plan.Statistics.ByFromDomain[domain]++
	}
	analysis.PlanEligible = plan.Statistics.TotalCount
	finalizeRates(analysis.ByCategory)
	finalizeRates(analysis.ByFromDomain)
	finalizeRates(analysis.ByAgeBucket)
	finalizeRates(analysis.BySizeBucket)
	sort.Slice(plan.Items, func(i, j int) bool {
		if plan.Items[i].Category == plan.Items[j].Category {
			return plan.Items[i].Date.After(plan.Items[j].Date)
		}
		return plan.Items[i].Category < plan.Items[j].Category
	})
	return plan, analysis
}

// excludeReason returns why a classified message must stay out of the cleanup
// plan, or "" when it is eligible. The flagged check has no override: a starred
// message is an explicit human signal that outranks every rule.
func excludeReason(message index.Message, category string, confidence float64, date, now time.Time, opts Options) string {
	if hasFlag(message.Flags, `\Flagged`) {
		return "flagged"
	}
	if len(opts.Folders) > 0 && !opts.Folders[normalizeToken(message.Folder)] {
		return "folder_out_of_scope"
	}
	if !opts.Categories[normalizeToken(category)] {
		return "category_not_targeted"
	}
	if confidence < opts.MinConfidence {
		return "low_confidence"
	}
	if !opts.OlderThan.IsZero() && !date.Before(opts.OlderThan) {
		return "too_recent"
	}
	return ""
}

func classify(message index.Message, rules []Rule) (string, float64, string, []string) {
	headerText := "List-Unsubscribe: " + message.ListUnsubscribe + "\nPrecedence: " + message.Precedence
	for _, rule := range rules {
		if matches(rule.headerRE, headerText) && matches(rule.fromRE, message.FromAddr) && matches(rule.subjectRE, message.Subject) {
			evidence := []string{"custom_rule:" + defaultString(rule.Name, rule.Category)}
			return rule.Category, rule.Confidence, rule.Reason, evidence
		}
	}
	if message.ListUnsubscribe != "" {
		return "marketing", .98, "List-Unsubscribe header is present", []string{"header:List-Unsubscribe"}
	}
	if strings.EqualFold(strings.TrimSpace(message.Precedence), "bulk") || strings.EqualFold(strings.TrimSpace(message.Precedence), "list") {
		return "marketing", .92, "bulk/list precedence header", []string{"header:Precedence=" + message.Precedence}
	}
	if verificationPattern.MatchString(message.Subject) {
		return "verification", .96, "verification-code subject pattern", []string{"subject_pattern:verification_code"}
	}
	domain := fromDomain(message.FromAddr)
	if domainListed(domain, socialDomains) {
		return "social_notification", .86, "known social notification sender domain", []string{"from_domain:" + domain}
	}
	if domainListed(domain, receiptDomains) {
		return "receipt", .84, "known billing or receipt sender domain", []string{"from_domain:" + domain}
	}
	if noreplyPattern.MatchString(message.FromAddr) {
		return "machine_notification", .82, "no-reply sender pattern", []string{"from_pattern:noreply"}
	}
	return "other", .30, "no deterministic rule matched", []string{"fallback:other"}
}

func compileBounded(value string) (*regexp.Regexp, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) > 1024 {
		return nil, fmt.Errorf("regex exceeds 1024 bytes")
	}
	return regexp.Compile(value)
}

func matches(expression *regexp.Regexp, value string) bool {
	return expression == nil || expression.MatchString(value)
}

func fromDomain(address string) string {
	address = strings.ToLower(strings.TrimSpace(address))
	index := strings.LastIndexByte(address, '@')
	if index < 0 || index == len(address)-1 {
		return ""
	}
	return strings.TrimSuffix(address[index+1:], ".")
}

func domainListed(domain string, values map[string]bool) bool {
	for domain != "" {
		if values[domain] {
			return true
		}
		index := strings.IndexByte(domain, '.')
		if index < 0 {
			break
		}
		domain = domain[index+1:]
	}
	return false
}

func ageBucket(now, date time.Time) string {
	if date.IsZero() {
		return "unknown"
	}
	days := now.Sub(date).Hours() / 24
	switch {
	case days < 7:
		return "0-7d"
	case days < 30:
		return "8-30d"
	case days < 90:
		return "31-90d"
	case days < 365:
		return "91-365d"
	default:
		return "365d+"
	}
}

func sizeBucket(size int64) string {
	switch {
	case size < 10<<10:
		return "<10KiB"
	case size < 100<<10:
		return "10-100KiB"
	case size < 1<<20:
		return "100KiB-1MiB"
	default:
		return "1MiB+"
	}
}

func hasFlag(flags []string, want string) bool {
	for _, flag := range flags {
		if strings.EqualFold(flag, want) {
			return true
		}
	}
	return false
}

func addBucket(values map[string]Bucket, key string, size int64, unread bool) {
	bucket := values[key]
	bucket.Count++
	bucket.SizeBytes += size
	if unread {
		bucket.UnreadCount++
	}
	values[key] = bucket
}

func finalizeRates(values map[string]Bucket) {
	for key, bucket := range values {
		if bucket.Count > 0 {
			bucket.UnreadRate = float64(bucket.UnreadCount) / float64(bucket.Count)
		}
		values[key] = bucket
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
