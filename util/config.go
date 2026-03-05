package util

import (
	"fmt"
	"keyop/core"
)

// ValidateConfig checks required channels are present and returns logging errors for any problems.
func ValidateConfig(pubSubType string, chanInfoMap map[string]core.ChannelInfo, required []string, logger core.Logger) []error {

	var errs []error

	if chanInfoMap == nil {
		err := fmt.Errorf("required field '%s' is empty", pubSubType)
		if logger != nil {
			logger.Error(err.Error())
		}
		errs = append(errs, err)
	} else {
		for _, req := range required {
			reqChanInfo, reqChanExists := chanInfoMap[req]
			if !reqChanExists {
				err := fmt.Errorf("required %s channel '%s' is missing", pubSubType, req)
				if logger != nil {
					logger.Error(err.Error())
				}
				errs = append(errs, err)
			} else {
				// Ensure channel has a name
				if reqChanInfo.Name == "" {
					err := fmt.Errorf("required %s channel '%s' is missing a name", pubSubType, req)
					if logger != nil {
						logger.Error(err.Error())
					}
					errs = append(errs, err)
				}
			}
		}
	}

	return errs
}
