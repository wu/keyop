package util

import (
	"fmt"
	"keyop/core"
)

func ValidateConfig(pubSubType string, chanInfoMap map[string]core.ChannelInfo, required []string) []error {

	var errs []error

	if chanInfoMap == nil {
		errs = append(errs, fmt.Errorf("required field '%s' is empty", pubSubType))
	} else {
		for _, req := range required {
			reqChanInfo, reqChanExists := chanInfoMap[req]
			if !reqChanExists {
				errs = append(errs, fmt.Errorf("required %s channel '%s' is missing", pubSubType, req))
			} else {
				// Ensure channel has a name
				if reqChanInfo.Name == "" {
					errs = append(errs, fmt.Errorf("required %s channel '%s' is missing a name", pubSubType, req))
				}
			}
		}
	}

	return errs
}
