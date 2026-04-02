package handlers

import "log"

func logDBError(endpoint, operation string, err error) {
	if err == nil {
		return
	}
	log.Printf("[db-error] endpoint=%s op=%s err=%v", endpoint, operation, err)
}

