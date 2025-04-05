package internal

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
)

func GetFreeMemoryMiB() (int64, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				freeMemoryKB, err := strconv.ParseInt(fields[1], 10, 64)
				freeMemoryMiB := freeMemoryKB / 1024
				if err != nil {
					return 0, err
				}
				return freeMemoryMiB, nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, err
	}

	return 0, errors.New("MemAvailable not found in /proc/meminfo")
}
