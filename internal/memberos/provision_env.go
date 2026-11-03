package memberos

import (
	"fmt"
	"os"
	"strconv"
)

func positiveEnvID(name string) (int64, error) {
	id, err := strconv.ParseInt(os.Getenv(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return id, nil
}

func positiveOrNegativeEnvID(name string) (int64, error) {
	id, err := strconv.ParseInt(os.Getenv(name), 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("%s must be a nonzero integer", name)
	}
	return id, nil
}
