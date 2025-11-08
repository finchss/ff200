package ff200

import "time"

func getOptions(opts []*Options) *Options {
	if len(opts) > 0 && opts[0] != nil {
		if opts[0].Timeout == 0 {
			opts[0].Timeout = 30 * time.Second
		}
		return opts[0]
	}
	return &Options{
		Timeout: 30 * time.Second,
	}
}
