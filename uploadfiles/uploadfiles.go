/* Copyright <2018> <Wilyarti Howard>
*
* Redistribution and use in source and binary forms, with or without modification, are permitted provided that the following conditions are met:
*
* 1. Redistributions of source code must retain the above copyright notice, this list of conditions and the following disclaimer.
*
* 2. Redistributions in binary form must reproduce the above copyright notice, this list of conditions and the following disclaimer in the documentatio
* n and/or other materials provided with the distribution.
*
* 3. Neither the name of the copyright holder nor the names of its contributors may be used to endorse or promote products derived from this software w
* ithout specific prior written permission.
*
* THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
* IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE
* LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE
* GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRIC
* T LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SU
* CH DAMAGE.
 */
package uploadfiles

import (
	"errors"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/minio/minio-go"
	"github.com/minio/sio"
	"golang.org/x/crypto/argon2"
)

/*
 * Minio Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2018 Minio, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

const (
	// SSE DARE package block size.
	sseDAREPackageBlockSize = 64 * 1024 // 64KiB bytes

	// SSE DARE package meta padding bytes.
	sseDAREPackageMetaSize = 32 // 32 bytes
)

// MAX number of concurrent uploads
// more has the effect of degrading performance
// 3 is about the maximum I recommend.
const MAX = 3

// errorString is a trivial implementation of error.
type errorString struct {
	s string
}

// New returns an error that formats as the given text.
func New(text string) error {
	return &errorString{text}
}
func (e *errorString) Error() string {
	return e.s
}

// EncryptedSize returns the size of the object after encryption.
// An encrypted object is always larger than a plain object
// except for zero size objects.
func getEncryptedSize(size int64) int64 {
	ssize := (size / sseDAREPackageBlockSize) * (sseDAREPackageBlockSize + sseDAREPackageMetaSize)
	if mod := size % (sseDAREPackageBlockSize); mod > 0 {
		ssize += mod + sseDAREPackageMetaSize
	}
	return ssize
}

// Upload will upload a map of files with the following format:
// hash -> filepath
func Upload(url string, port int, secure bool, accesskey string, secretkey string, enckey string, filelist map[string]string, bucket string, consolebufptr *[]byte, curptr *int32, msgbufptr *[]byte) ([]string, error) {
	// break up map into 5 parts
	jobs := make(chan map[string]string, MAX)
	results := make(chan string, len(filelist))
	*curptr = 0

	// This starts up MAX workers, initially blocked
	// because there are no jobs yet.
	for w := 1; w <= MAX; w++ {
		go uploadfile(bucket, url, secure, accesskey, secretkey, enckey, w, jobs, int32(len(filelist)), consolebufptr, curptr, msgbufptr, results)
	}

	// Here we send MAX `jobs` and then `close` that
	// channel to indicate that's all the work we have.
	for hash, filepath := range filelist {
		job := make(map[string]string)
		job[hash] = filepath
		jobs <- job
	}
	close(jobs)

	var grmsgs []string
	var failed []string
	// Finally we collect all the results of the work.
	for a := 1; a <= len(filelist); a++ {
		grmsgs = append(grmsgs, <-results)
	}
	close(results)
	var count float64
	var errCount float64
	for _, msg := range grmsgs {
		if msg != "" {
			errCount++
			failed = append(failed, msg)
		} else {
			count++
		}
	}
	if errCount != 0 {
		out := fmt.Sprintf("Failed to upload: %v files", errCount)
		*consolebufptr = []byte(fmt.Sprintln(out))

		return failed, errors.New(out)
	}
	return failed, nil

}

func uploadfile(bucket string, url string, secure bool, accesskey string, secretkey string, enckey string, id int, jobs <-chan map[string]string, numjobs int32, consolebufptr *[]byte, curptr *int32, msgbufptr *[]byte, results chan<- string) {
	for j := range jobs {
		for hash, filepath := range j {
			s3Client, err := minio.New(url, accesskey, secretkey, secure)
			// break unrecoverable errors
			if err != nil {
				out := fmt.Sprintf("[F] %s => %s failed to upload: %s", hash, filepath, err)
				fmt.Println(out)
				*consolebufptr = []byte(fmt.Sprintln(out))
				*curptr = (*curptr + (1000000 / numjobs))
				results <- hash
				break
			}

			// minio-go example code modified:
			object, err := os.Open(filepath)
			if err != nil {
				out := fmt.Sprintf("[F] %s => %s failed to upload: %s", hash, filepath, err)
				fmt.Println(out)
				*consolebufptr = []byte(fmt.Sprintln(out))
				*curptr = (*curptr + (1000000 / numjobs))
				results <- hash
				break
			}
			defer object.Close()
			objectStat, err := object.Stat()
			if err != nil {
				out := fmt.Sprintf("[F] %s => %s failed to upload: %s", hash, filepath, err)
				fmt.Println(out)
				*consolebufptr = []byte(fmt.Sprintln(out))
				*curptr = (*curptr + (1000000 / numjobs))
				results <- hash
				break
			}
			password := []byte(enckey)
			salt := []byte(path.Join(bucket, hash))
			encrypted, err := sio.EncryptReader(object, sio.Config{
				// generate a 256 bit long key.
				Key: argon2.IDKey(password, salt, 1, 64*1024, 4, 32),
			})
			if err != nil {
				out := fmt.Sprintf("[F] %s => %s failed to upload: %s", hash, filepath, err)
				fmt.Println(out)
				*consolebufptr = []byte(fmt.Sprintln(out))
				*curptr = (*curptr + (1000000 / numjobs))
				results <- hash
				break
			}

			// Encrypt file content and upload to the server
			// try multiple times
			b := path.Base(filepath)
			for i := 0; i < 4; i++ {
				start := time.Now()
				size, err := s3Client.PutObject(bucket, hash, encrypted, getEncryptedSize(objectStat.Size()), minio.PutObjectOptions{})
				elapsed := time.Since(start)
				if err != nil {
					if i == 3 {
						out := fmt.Sprintf("[F] %s => %s failed to upload: %s", hash, filepath, err)
						fmt.Println(out)
						*consolebufptr = []byte(fmt.Sprintln(out))
						*curptr = (*curptr + (1000000 / numjobs))
						results <- hash
						break
					}
				} else {
					var s uint64 = uint64(size)
					if len(hash) == 64 {
						out := fmt.Sprintf("(%s)(%s) %s => %s\n", elapsed, humanize.Bytes(s), hash[:8], b)
						fmt.Print(out)
						*curptr = (*curptr + (1000000 / numjobs))
						*msgbufptr = []byte(fmt.Sprintln(out))

					} else {
						out := fmt.Sprintf("(%s)(%s) %s => %s\n", elapsed, humanize.Bytes(s), hash, b)
						fmt.Print(out)
						*curptr = (*curptr + (1000000 / numjobs))
						*msgbufptr = []byte(fmt.Sprintln(out))
					}
					results <- ""
					break
				}
				time.Sleep(time.Duration(i) * time.Second)
			}
		}
	}
}