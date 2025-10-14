package util

import (
	"fmt"
	"keyop/core"
)

func ValidateConfig(pubSubType string, chanInfoMap map[string]core.ChannelInfo, required []string, logger core.Logger) []error {

	var errs []error

	if chanInfoMap == nil {
		err := fmt.Errorf("required field '%s' is empty", pubSubType)
		logger.Error(err.Error())
		errs = append(errs, err)
	} else {
		for _, req := range required {
			reqChanInfo, reqChanExists := chanInfoMap[req]
			if !reqChanExists {
				err := fmt.Errorf("required %s channel '%s' is missing", pubSubType, req)
				logger.Error(err.Error())
				errs = append(errs, err)
			} else {
				// Ensure channel has a name
				if reqChanInfo.Name == "" {
					err := fmt.Errorf("required %s channel '%s' is missing a name", pubSubType, req)
					logger.Error(err.Error())
					errs = append(errs, err)
				}
			}
		}
	}

	return errs
}
