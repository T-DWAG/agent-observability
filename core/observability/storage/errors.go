package storage

import "errors"

var (
	ErrorNotFound         = errors.New("storage: not found")
	ErrorEvaluationExists = errors.New("storage: evaluation already exists")
)
