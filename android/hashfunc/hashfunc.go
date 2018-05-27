package hashfunc

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hashtree-mobile/hashfiles"
	"hashtree-mobile/readdb"
	"hashtree-mobile/writedb"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/minio/minio-go"
	"github.com/minio/sio"
	"github.com/pierrec/lz4"
	"golang.org/x/crypto/argon2"
)

// callback
var jc JavaCallback

type JavaCallback interface {
	SendString(string)
}

func RegisterJavaCallback(c JavaCallback) {
	jc = c
}
func TestCall() {
	jc.SendString("foo")

}

const MAX = 2

const (
	// SSE DARE package block size.
	sseDAREPackageBlockSize = 64 * 1024 // 64KiB bytes

	// SSE DARE package meta padding bytes.
	sseDAREPackageMetaSize = 32 // 32 bytes
)

func decryptedSize(encryptedSize int64) (int64, error) {
	if encryptedSize == 0 {
		return encryptedSize, nil
	}
	size := (encryptedSize / (sseDAREPackageBlockSize + sseDAREPackageMetaSize)) * sseDAREPackageBlockSize
	if mod := encryptedSize % (sseDAREPackageBlockSize + sseDAREPackageMetaSize); mod > 0 {
		if mod < sseDAREPackageMetaSize+1 {
			return -1, errors.New("object is tampered")
		}
		size += mod - sseDAREPackageMetaSize
	}
	return size, nil
}

// errorString is a trivial implementation of error.
type errorString struct {
	s string
}

//

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

// compressLZ4 returns an io.Reader that produces lz4 compressed data from src.
func compressLZ4(src io.Reader) io.Reader {
	pr, pw := io.Pipe()
	zw := lz4.NewWriter(pw)
	go func() {
		_, err := zw.ReadFrom(src)
		pw.CloseWithError(err) // make sure the other side can see EOF or other errors
	}()
	return pr
}

// decompressLZ4 returns an io.Reader that produces lz4 compressed data from src.
func decompressLZ4(src io.Reader) io.Reader {
	pr, pw := io.Pipe()
	zr := lz4.NewReader(src)
	go func() {
		_, err := zr.WriteTo(pw)
		pw.CloseWithError(err) // make sure the other side can see EOF or other errors
	}()
	return pr

}

// InitRepo creates the encrypted db file and creates the bucket
func Initrepo(server string, secure bool, accesskey string, secretkey string, enckey string, bucketname string, dir string) bool {
	// New returns an Amazon S3 compatible client object. API compatibility (v2 or v4) is automatically
	// determined based on the Endpoint value.
	s3Client, err := minio.New(server, accesskey, secretkey, secure)
	if err != nil {
		jc.SendString(fmt.Sprintln(err))
		return false
	}

	found, err := s3Client.BucketExists(bucketname)
	if err != nil {
		jc.SendString(fmt.Sprintln(err))
		return false
	}

	if found {
		jc.SendString("Bucket exists.")
	} else {
		jc.SendString("Creating bucket.")
		err = s3Client.MakeBucket(bucketname, "us-east-1")
		if err != nil {
			jc.SendString(fmt.Sprintln(err))
			return false
		}
	}
	var strs []string
	slash := dir[len(dir)-1:]
	if slash != "/" {
		strs = append(strs, dir)
		strs = append(strs, "/")
		dir = strings.Join(strs, "")
	}
	var dbname []string
	var dbnameLocal []string
	dbname = append(dbname, bucketname)
	dbname = append(dbname, ".db")
	dbnameLocal = append(dbnameLocal, dir)
	dbnameLocal = append(dbnameLocal, ".")
	dbnameLocal = append(dbnameLocal, strings.Join(dbname, ""))
	// check if dir exists, create if not
	basedir := filepath.Dir(strings.Join(dbnameLocal, ""))
	os.MkdirAll(basedir, os.ModePerm)

	// create empty repository
	file, err := os.Create(strings.Join(dbnameLocal, ""))
	defer file.Close()
	if err != nil {
		jc.SendString(fmt.Sprintln(err))
		return false
	}
	dbuploadlist := make(map[string]string)
	// add these files to the upload list
	dbuploadlist[strings.Join(dbname, "")] = strings.Join(dbnameLocal, "")
	failedUploads, err := Upload(server, 443, secure, accesskey, secretkey, enckey, dbuploadlist, bucketname)
	if err != nil {
		for _, hash := range failedUploads {
			jc.SendString(fmt.Sprintln("Failed to upload: ", hash))
		}
		return false
	}

	err = os.Remove(strings.Join(dbnameLocal, ""))
	if err != nil {
		jc.SendString(fmt.Sprintln("Error deleting database!", err))
	}
	return true

}

// Hashlist returns list of snapshots
func Hashlist(url string, secure bool, accesskey string, secretkey string, bucket string) string {
	log.SetFlags(log.Lshortfile)

	// New returns an Amazon S3 compatible client object. API compatibility (v2 or v4) is automatically
	// determined based on the Endpoint value.
	s3Client, err := minio.New(url, accesskey, secretkey, secure)
	if err != nil {
		jc.SendString(fmt.Sprint(err))
		return "ERROR"
	}
	// Create a done channel to control 'ListObjects' go routine.
	doneCh := make(chan struct{})

	// Indicate to our routine to exit cleanly upon return.
	defer close(doneCh)

	// List all objects from a bucket-name with a matching prefix.
	var snapshots []string
	for object := range s3Client.ListObjects(bucket, "", secure, doneCh) {
		if object.Err != nil {
			jc.SendString(fmt.Sprint(object.Err))
			return "ERROR"
		}
		matched, err := regexp.MatchString(".hsh$", object.Key)
		if err != nil {
			return "ERROR"
		}
		if matched == true {
			snapshots = append(snapshots, object.Key)
			snapshots = append(snapshots, "\n")
		}
	}
	if len(snapshots) > 0 {
		return strings.Join(snapshots, "\n")
	}
	return "ERROR"
}

// Hashseed deploys a hash tree data structure to a directory creating
// downloading all the files and verifying the SHA256 hash
func Hashseed(server string, accesskey string, secretkey string, enckey string, databasename string, bucketname string, secure bool, dir string, nuke bool) bool {
	log.SetFlags(log.Lshortfile)
	// check for and add trailing / in folder name
	var strs []string

	slash := dir[(len(dir))-1:]
	if slash != "/" {
		strs = append(strs, dir)
		strs = append(strs, "/")
		dir = strings.Join(strs, "")
	}

	// download .db from server this contains the hashed
	var dbnameLocal []string
	dbnameLocal = append(dbnameLocal, dir)
	dbnameLocal = append(dbnameLocal, databasename)
	downloadlist := make(map[string]string)
	downloadlist[strings.Join(dbnameLocal, "")] = databasename

	// download and check error
	fmt.Println(dbnameLocal)
	var remotedb = make(map[string][]string)
	_, err := Download(server, 443, secure, accesskey, secretkey, enckey, downloadlist, bucketname, nuke)
	if err != nil {
		fmt.Println("Error unable to download database:", err)
		jc.SendString(fmt.Sprintln("Error unable to download database!", err))
	} else {
		remotedb, err = readdb.Load(strings.Join(dbnameLocal, ""))
		if err != nil {
			jc.SendString(fmt.Sprintln("Error unable to download database!", err))
			return false
		}
	}
	// iterate through hashmap, pull list of file names
	// build these into a hash => path list
	dlist := make(map[string]string)
	for hash, filearray := range remotedb {
		// build local file tree
		for _, file := range filearray {
			var f []string
			f = append(f, dir)
			f = append(f, file)
			dlist[strings.Join(f, "")] = hash
		}
	}
	// Download files
	failedDownloads, err := Download(server, 443, secure, accesskey, secretkey, enckey, dlist, bucketname, nuke)
	if err != nil {
		for _, file := range failedDownloads {
			fmt.Println("Error failed to download: ", file)
			jc.SendString(fmt.Sprintln(err))
		}
	}
	jc.SendString(fmt.Sprint("Successfully downloaded ", (len(dlist) - len(failedDownloads)), " files. ", len(failedDownloads), " failed."))
	if len(failedDownloads) > 0 {
		return false
	}
	return true
}

// Hashtree generates a data structure of the specified directory and uploads
// the files that are missing remotly as well as a snapshot of the directory in
// time.
func Hashtree(server string, accesskey string, secretkey string, enckey string, bucketname string, secure bool, dir string) bool {
	log.SetFlags(log.Lshortfile)
	// check for and add trailing / in folder name
	var strs []string

	slash := dir[(len(dir))-1:]
	if slash != "/" {
		strs = append(strs, dir)
		strs = append(strs, "/")
		dir = strings.Join(strs, "")
	}

	// create various variables
	var hashmap = make(map[string][]string)
	var remotedb = make(map[string][]string)
	// create hash database name
	var hashdb []string
	hashdb = append(hashdb, dir)
	hashdb = append(hashdb, ".")
	hashdb = append(hashdb, bucketname)
	hashdb = append(hashdb, ".hsh")
	// the default output of files is a byte array and string
	// this is later changed to string[]=>string
	var files = make(map[string][sha256.Size]byte)

	// scan files and return map filepath = hash
	files = hashfiles.Scan(dir)
	jc.SendString(fmt.Sprint("Files scanned: ", len(files)))

	// download .db from server this contains the hashed
	// of all already uploaded files
	// it will be appended to and reuploaded with new hashed at the end
	var dbname []string
	var dbnameLocal []string
	dbname = append(dbname, bucketname)
	dbname = append(dbname, ".db")
	dbnameLocal = append(dbnameLocal, dir)
	dbnameLocal = append(dbnameLocal, ".")
	dbnameLocal = append(dbnameLocal, strings.Join(dbname, ""))
	downloadlist := make(map[string]string)
	downloadlist[strings.Join(dbnameLocal, "")] = strings.Join(dbname, "")

	// download and check error
	// download has the format filename => remotename
	failedDownloads, err := Download(server, 443, secure, accesskey, secretkey, enckey, downloadlist, bucketname, true)
	if err != nil {
		for _, file := range failedDownloads {
			jc.SendString(fmt.Sprint(err))
			jc.SendString(fmt.Sprint("Error failed to download: ", file))
		}
		jc.SendString(fmt.Sprint(err))
		jc.SendString("Error: Unable to download database. Hash the database been initialised?")
		return false
	}
	// read database
	remotedb, err = readdb.Load(strings.Join(dbnameLocal, ""))
	if err != nil {
		jc.SendString(fmt.Sprint("Unable to read database!", err))
		return false
	}

	// create out map of [sha256hash] => array of file names
	for file, hash := range files {
		// build local file tree
		s := hex.EncodeToString(hash[:])
		v := hashmap[hex.EncodeToString(hash[:])]
		if len(v) == 0 {
			hashmap[s] = append(hashmap[s], file)
		} else {
			hashmap[s] = append(hashmap[s], file)
		}
	}
	// create map of files for upload
	// do this with the full path of each file before it's
	// modified below.
	var c float64
	uploadlist := make(map[string]string)
	for hash, filearray := range hashmap {
		// convert hex to ascii
		// use first file in list for upload
		v := remotedb[hash]
		// check if database filenames
		if filearray[0] == strings.Join(hashdb, "") {
			continue
		} else if filearray[0] == strings.Join(dbnameLocal, "") {
			continue
			// this file exist remotely
		} else if len(v) == 0 {
			uploadlist[hash] = filearray[0]
			// file exists remotely
		} else {
			c += float64(len(v))
			//for _, _ := range filearray {
			//b := path.Base(filename)
			//fmt.Printf("Parsing database: %v\t %s", c, b)
			//	c++
			//}
		}

	}
	jc.SendString(fmt.Sprint("Verified files: ", c))
	// write database to file
	// before writing remove directory prefix
	// so the files in the directory become the root of the data structure
	var hashmapcooked = make(map[string][]string)

	for hash, filearray := range hashmap {
		for _, file := range filearray {
			var reg []string
			reg = append(reg, "^")
			reg = append(reg, dir)
			var re = regexp.MustCompile(strings.Join(reg, ""))
			f := re.ReplaceAllString(file, "")
			hashmapcooked[hash] = append(hashmapcooked[hash], f)

		}
	}
	// add extra file to remotedb before uploading it
	for file, hash := range files {
		// update remotedb with new files
		s := hex.EncodeToString(hash[:])
		v := remotedb[s]
		// remote base name

		if len(v) == 0 {
			remotedb[s] = append(remotedb[s], file)
		} else {
			remotedb[s] = append(remotedb[s], file)
		}
		remotedb[s] = removeDuplicates(remotedb[s])

	}

	// upload and check error
	failedUploads, err := Upload(server, 443, secure, accesskey, secretkey, enckey, uploadlist, bucketname)
	if err != nil {
		for _, hash := range failedUploads {
			// remove failed uploads from database
			jc.SendString(fmt.Sprint("Failed to upload: ", hash))

			delete(remotedb, hash)
			delete(hashmapcooked, hash)

		}
		jc.SendString(fmt.Sprint(err))
	}
	jc.SendString(fmt.Sprint("Successfully uploaded ", (len(uploadlist) - len(failedUploads)), " files. ", len(failedUploads), " failed."))
	// create database and upload
	t := time.Now()
	// create a snapshot of the database
	// create a snapshot of the hash tree
	var reponame []string
	var dbsnapshot []string
	dbsnapshot = append(dbsnapshot, bucketname)
	dbsnapshot = append(dbsnapshot, "-")
	dbsnapshot = append(dbsnapshot, t.Format("2006-01-02_15:04:05"))
	dbsnapshot = append(dbsnapshot, ".db")

	reponame = append(reponame, bucketname)
	reponame = append(reponame, "-")
	reponame = append(reponame, t.Format("2006-01-02_15:04:05"))
	reponame = append(reponame, ".hsh")

	// write localdb to hard drive
	err = writedb.Dump(strings.Join(hashdb, ""), hashmapcooked)
	if err != nil {
		jc.SendString(fmt.Sprint("Error writing to database!", err))
		return false
	}

	// write remotedb to file
	err = writedb.Dump(strings.Join(dbnameLocal, ""), remotedb)
	if err != nil {
		jc.SendString(fmt.Sprint("Error writing to database!", err))
		return false
	}

	dbuploadlist := make(map[string]string)
	// add these files to the upload list
	dbuploadlist[strings.Join(reponame, "")] = strings.Join(hashdb, "")
	dbuploadlist[strings.Join(dbname, "")] = strings.Join(dbnameLocal, "")
	dbuploadlist[strings.Join(dbsnapshot, "")] = strings.Join(dbnameLocal, "")
	failedUploads, err = Upload(server, 443, secure, accesskey, secretkey, enckey, dbuploadlist, bucketname)
	if err != nil {
		for _, hash := range failedUploads {
			jc.SendString(fmt.Sprint("Failed to upload: ", hash))
		}
		jc.SendString(fmt.Sprint(err))
		err = os.Remove(strings.Join(hashdb, ""))
		err = os.Remove(strings.Join(dbnameLocal, ""))
		if err != nil {
			jc.SendString(fmt.Sprint("Error deleting database!", err))
		}
		return false
	}

	err = os.Remove(strings.Join(hashdb, ""))
	err = os.Remove(strings.Join(dbnameLocal, ""))
	if err != nil {
		jc.SendString(fmt.Sprint("Error deleting database!", err))
		return false
	}
	return true
}

func removeDuplicates(elements []string) []string {
	// Use map to record duplicates as we find them.
	encountered := map[string]bool{}
	result := []string{}

	for v := range elements {
		if encountered[elements[v]] == true {
			// Do not add duplicate.
		} else {
			// Record this element as an encountered element.
			encountered[elements[v]] = true
			// Append to result slice.
			result = append(result, elements[v])
		}
	}
	// Return the new slice.
	return result
}

// Download a list of file in format name => dest
func Download(url string, port int, secure bool, accesskey string, secretkey string, enckey string, filelist map[string]string, bucket string, nuke bool) ([]string, error) {
	// break up map into 5 parts
	jobs := make(chan map[string]string, MAX)
	results := make(chan string, len(filelist))
	// reset progress bar

	// This starts up MAX workers, initially blocked
	// because there are no jobs yet.
	for w := 1; w <= MAX; w++ {
		go downloadfile(bucket, url, secure, accesskey, secretkey, enckey, w, nuke, jobs, int32(len(filelist)), results)
	}

	// Here we send MAX `jobs` and then `close` that
	// channel to indicate that's all the work we have.
	for hash, fpath := range filelist {
		job := make(map[string]string)
		job[hash] = fpath
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
	//fmt.Println(count, " files downloaded successfully.")

	if errCount != 0 {
		out := fmt.Sprintf("Failed to download: %v files", errCount)
		fmt.Println(out)
		return failed, errors.New(out)
	}
	return failed, nil

}

func downloadfile(bucket string, url string, secure bool, accesskey string, secretkey string, enckey string, id int, nuke bool, jobs <-chan map[string]string, numjobs int32, results chan<- string) {
	for j := range jobs {
		// hash is reversed: filepath => hash
		for fpath, hash := range j {
			if _, err := os.Stat(fpath); err == nil {
				data, err := ioutil.ReadFile(fpath)
				if err != nil {
					out := fmt.Sprintf("[!] %s => %s failed to verify: %s", hash, fpath, err)
					fmt.Println(out)
					jc.SendString(out)
					results <- hash
					break
				}

				digest := sha256.Sum256(data)
				checksum := hex.EncodeToString(digest[:])
				if hash == checksum {
					b := path.Base(fpath)
					out := fmt.Sprintf("[V]\t%s => %s", hash[:8], b)
					fmt.Println(out)
					jc.SendString(out)
					results <- ""
					break
				} else if (hash != checksum) && (nuke == false) {
					out := fmt.Sprintf("[!] %s => %s local file differs from remote version!", hash, fpath)
					fmt.Println(out)
					jc.SendString(out)
					results <- hash
					break

				}
			}
			// create directorys for files:
			// create file path:
			b := path.Base(fpath)
			basedir := filepath.Dir(fpath)
			os.MkdirAll(basedir, os.ModePerm)
			////
			// minio-go download object code:
			// Encrypt file content and upload to the server
			// try multiple times
			for i := 0; i < 3; i++ {
				s3Client, err := minio.New(url, accesskey, secretkey, secure)
				// break unrecoverable errors
				if err != nil {
					out := fmt.Sprintf("[!] %s => %s failed to download: %s", hash, fpath, err)
					fmt.Println(out)
					jc.SendString(out)
					results <- hash
					break
				}
				start := time.Now()
				obj, err := s3Client.GetObject(bucket, hash, minio.GetObjectOptions{})
				if err != nil {
					out := fmt.Sprintf("[!] %s => %s failed to download: %s", hash, fpath, err)
					fmt.Println(out)
					jc.SendString(out)
					if i == 2 {
						results <- hash
						break
					}
					continue
				}
				// size not used. we don't know the decrypted and decompressed
				// size so just ignore it. errors will be caught by the hash
				// check.
				/*size, err := decryptedSize(objSt.Size)

				objSt, err := obj.Stat()
				if err != nil {
					out := fmt.Sprintf("[!] %s => %s failed to download: %s", hash, fpath, err)
					fmt.Println(out)
					results <- hash
					break
				}
				if err != nil {
					out := fmt.Sprintf("[!] %s => %s failed to download: %s", hash, fpath, err)
					fmt.Println(out)
					results <- hash
					break
				}*/
				localFile, err := os.Create(fpath)
				if err != nil {
					out := fmt.Sprintf("[!] %s => %s Error creating file.", hash, fpath)
					fmt.Println(out)
					jc.SendString(out)
					results <- hash
					break
				}
				defer localFile.Close()

				password := []byte(enckey)              // Change as per your needs.
				salt := []byte(path.Join(bucket, hash)) // Change as per your needs.
				decrypted, err := sio.DecryptReader(obj, sio.Config{
					// generate a 256 bit long key.
					Key: argon2.IDKey(password, salt, 1, 64*1024, 4, 32),
				})
				if err != nil {
					out := fmt.Sprintf("[!] %s => %s failed to decrpyt file: %s", hash, fpath, err)
					fmt.Println(out)
					jc.SendString(out)
					if i == 2 {
						results <- hash
						break
					}
					continue
				}
				pr := decompressLZ4(decrypted)
				dsize, err := io.Copy(localFile, pr)
				if err != nil {
					out := fmt.Sprintf("[!] %s => %s failed to decompress file: %s", hash, fpath, err)
					fmt.Println(out)
					jc.SendString(out)
					if i == 2 {
						results <- hash
						break
					}
					continue
				}
				elapsed := time.Since(start).Seconds()
				var s uint64 = uint64(dsize)
				if len(hash) == 64 {
					data, err := ioutil.ReadFile(fpath)
					if err != nil {
						out := fmt.Sprintf("[!] %s => %s failed to download: %s", hash, fpath, err)
						fmt.Println(out)
						jc.SendString(out)
						results <- hash
						break
					}

					digest := sha256.Sum256(data)
					checksum := hex.EncodeToString(digest[:])
					if hash != checksum {
						out := fmt.Sprintf("[!] %s => %s checksum mismatch!", hash, fpath)
						fmt.Println(out)
						jc.SendString(out)
						results <- hash
						break

					}
					out := fmt.Sprintf("[D][V]\t(%.2fs)\t(%s)    \t%s => %s", elapsed, humanize.Bytes(s), hash[:8], b)
					fmt.Println(out)
					jc.SendString(out)
					results <- ""
					break

				} else {
					out := fmt.Sprintf("[D][%d]\t(%.2fs)\t(%s)    \t%s => %s", i, elapsed, humanize.Bytes(s), hash, b)
					fmt.Println(out)
					jc.SendString(out)
					results <- ""
					break
				}
			}

		}
	}
}

// Upload will upload a map of files with the following format:
// hash -> filepath
func Upload(url string, port int, secure bool, accesskey string, secretkey string, enckey string, filelist map[string]string, bucket string) ([]string, error) {
	// break up map into 5 parts
	jobs := make(chan map[string]string, MAX)
	results := make(chan string, len(filelist))

	// This starts up MAX workers, initially blocked
	// because there are no jobs yet.
	for w := 1; w <= MAX; w++ {
		go uploadfile(bucket, url, secure, accesskey, secretkey, enckey, w, jobs, int32(len(filelist)), results)
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
		fmt.Println(out)
		return failed, errors.New(out)
	}
	return failed, nil

}

func uploadfile(bucket string, url string, secure bool, accesskey string, secretkey string, enckey string, id int, jobs <-chan map[string]string, numjobs int32, results chan<- string) {
	for j := range jobs {
		for hash, filepath := range j {
			s3Client, err := minio.New(url, accesskey, secretkey, secure)
			// break unrecoverable errors
			if err != nil {
				out := fmt.Sprintf("[F] %s => %s failed to upload: %s", hash, filepath, err)
				fmt.Println(out)
				jc.SendString(out)
				results <- hash
				break
			}
			b := path.Base(filepath)
			for i := 0; i < 3; i++ {
				// minio-go example code modified:
				object, err := os.Open(filepath)
				if err != nil {
					out := fmt.Sprintf("[F] %s => %s failed to upload: %s", hash, filepath, err)
					fmt.Println(out)
					jc.SendString(out)
					results <- hash
					break
				}
				defer object.Close()
				objectStat, err := object.Stat()
				if err != nil {
					out := fmt.Sprintf("[F] %s => %s failed to upload: %s", hash, filepath, err)
					fmt.Println(out)
					jc.SendString(out)
					results <- hash
					break
				}
				password := []byte(enckey)
				salt := []byte(path.Join(bucket, hash))

				// Encrypt file content and upload to the server
				// try multiple times
				start := time.Now()

				pw := compressLZ4(object)
				encrypted, err := sio.EncryptReader(pw, sio.Config{
					// generate a 256 bit long key.
					Key: argon2.IDKey(password, salt, 1, 64*1024, 4, 32),
				})
				if err != nil {
					out := fmt.Sprintf("[F] %s => %s failed to upload: %s", hash, filepath, err)
					fmt.Println(out)
					jc.SendString(out)
					results <- hash
					break
				}

				// specify size as -1 as there is no way to determine the size
				size, err := s3Client.PutObject(bucket, hash, encrypted, -1, minio.PutObjectOptions{})
				if size == 0 && objectStat.Size() != 0 {
					out := fmt.Sprintf("[F] %s => %s failed to upload: %s", hash, filepath, err)
					fmt.Println(out)
					jc.SendString(out)
					results <- hash
					break
				}
				elapsed := time.Since(start).Seconds()
				if err != nil {
					if i == 2 {
						out := fmt.Sprintf("[F] %s => %s failed to upload: %s", hash, filepath, err)
						fmt.Println(out)
						jc.SendString(out)
						results <- hash
						break
					}
				} else {
					var s uint64 = uint64(size)
					if len(hash) == 64 {
						fmt.Printf("[U][%d]\t(%.2fs)\t(%s)    \t%s => %s\n", i, elapsed, humanize.Bytes(s), hash[:8], b)
						jc.SendString(fmt.Sprintf("[U][%d]\t(%.2fs)\t(%s)    \t%s => %s\n", i, elapsed, humanize.Bytes(s), hash[:8], b))

					} else {
						fmt.Printf("[U][%d]\t(%.2fs)\t(%s)    \t%s => %s\n", i, elapsed, humanize.Bytes(s), hash, b)
						jc.SendString(fmt.Sprintf("[U][%d]\t(%.2fs)\t(%s)    \t%s => %s\n", i, elapsed, humanize.Bytes(s), hash, b))

					}
					results <- ""
					break
				}
				time.Sleep(time.Duration(i) * time.Second)
			}
		}
	}
}
