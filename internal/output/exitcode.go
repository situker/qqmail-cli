package output

const (
	ExitOK                = 0
	ExitInternal          = 1
	ExitUsage             = 2
	ExitConfig            = 3
	ExitAuth              = 10
	ExitServiceNotEnabled = 11
	ExitNetwork           = 20
	ExitTLS               = 21
	ExitRateLimited       = 30
	ExitNotFound          = 40
	ExitPolicyDenied      = 50
	ExitParse             = 60
	ExitPartial           = 70
)
