package hashfunc

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hashtree-mobile/downloadfiles"
	"hashtree-mobile/hashfiles"
	"hashtree-mobile/readdb"
	"hashtree-mobile/uploadfiles"
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

	"github.com/minio/minio-go"
	"github.com/minio/sio"
	"golang.org/x/crypto/argon2"
)

type EventBus interface {
	SendEvent(channel, message string)
}

func test(bus EventBus) {
	bus.SendEvent("general", "After 3 seconds!")
}

// Hashlist returns list of snapshots
func Hashlist(url string, secure bool, accesskey string, secretkey string, bucket string) string {
	log.SetFlags(log.Lshortfile)

	// New returns an Amazon S3 compatible client object. API compatibility (v2 or v4) is automatically
	// determined based on the Endpoint value.
	s3Client, err := minio.New(url, accesskey, secretkey, secure)
	if err != nil {
		fmt.Println(err)
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
			fmt.Println(object.Err)
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

const MAX = 3

const (
	// SSE DARE package block size.
	sseDAREPackageBlockSize = 64 * 1024 // 64KiB bytes

	// SSE DARE package meta padding bytes.
	sseDAREPackageMetaSize = 32 // 32 bytes
)

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

// Download a list of file in format name => dest
func Download(url string, port int, secure bool, accesskey string, secretkey string, enckey string, fpath string, hash string, bucket string, nuke bool) string {
	// break up map into 5 parts
	jobs := make(chan map[string]string, MAX)
	results := make(chan string, 1)
	// reset progress bar

	// This starts up MAX workers, initially blocked
	// because there are no jobs yet.
	for w := 1; w <= MAX; w++ {
		go downloadfile(bucket, url, secure, accesskey, secretkey, enckey, w, nuke, jobs, results)
	}

	// Here we send MAX `jobs` and then `close` that
	// channel to indicate that's all the work we have.
	job := make(map[string]string)
	job[hash] = fpath
	jobs <- job
	close(jobs)

	var grmsgs []string
	var failed []string
	// Finally we collect all the results of the work.
	for a := 1; a <= 1; a++ {
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
		return strings.Join(failed, "")
	}
	return ""

}

func downloadfile(bucket string, url string, secure bool, accesskey string, secretkey string, enckey string, id int, nuke bool, jobs <-chan map[string]string, results chan<- string) {
	for j := range jobs {
		// hash is reversed: filepath => hash
		for hash, fpath := range j {
			fmt.Println("Downloading remote file: ", hash, "to: ", fpath)
			if _, err := os.Stat(fpath); err == nil {
				data, err := ioutil.ReadFile(fpath)
				if err != nil {
					out := fmt.Sprintf("[!] %s => %s failed to verify: %s", hash, fpath, err)
					fmt.Println(out)
					results <- hash
					break
				}

				digest := sha256.Sum256(data)
				checksum := hex.EncodeToString(digest[:])
				if hash == checksum {
					b := path.Base(fpath)
					out := fmt.Sprintf("[V]   %s => %s", hash[:8], b)
					fmt.Println(out)
					results <- ""
					break
				} else if (hash != checksum) && (nuke == false) {
					out := fmt.Sprintf("[!] %s => %s local file differs from remote version!", hash, fpath)
					fmt.Println(out)
					results <- hash
					break

				}
			}
			s3Client, err := minio.New(url, accesskey, secretkey, secure)
			// break unrecoverable errors
			if err != nil {
				out := fmt.Sprintf("[!] %s => %s failed to download: %s", hash, fpath, err)
				fmt.Println(out)
				results <- hash
				break
			}
			////
			// create directorys for files:
			// create file path:
			b := path.Base(fpath)
			basedir := filepath.Dir(fpath)
			os.MkdirAll(basedir, os.ModePerm)
			////
			// minio-go download object code:
			// Encrypt file content and upload to the server
			// try multiple times
			for i := 0; i < 4; i++ {
				start := time.Now()
				obj, err := s3Client.GetObject(bucket, hash, minio.GetObjectOptions{})
				if err != nil {
					if i == 3 {
						out := fmt.Sprintf("[!] %s => %s failed to download: %s", hash, fpath, err)
						fmt.Println(out)
						results <- hash
						break
					}
				}

				objSt, err := obj.Stat()
				if err != nil {
					out := fmt.Sprintf("[!] %s => %s failed to download: %s", hash, fpath, err)
					fmt.Println(out)
					results <- hash
					break
				}

				size, err := decryptedSize(objSt.Size)
				if err != nil {
					out := fmt.Sprintf("[!] %s => %s failed to download: %s", hash, fpath, err)
					fmt.Println(out)
					results <- hash
					break
				}
				localFile, err := os.Create(fpath)
				if err != nil {
					out := fmt.Sprintf("[!] %s => %s Error creating file.", hash, fpath)
					fmt.Println(out)
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
					out := fmt.Sprintf("[!] %s => %s failed to download: %s", hash, fpath, err)
					fmt.Println(out)
					results <- hash
					break
				}
				dsize, err := io.CopyN(localFile, decrypted, size)
				if err != nil {
					out := fmt.Sprintf("[!] %s => %s failed to download: %s", hash, fpath, err)
					fmt.Println(out)
					results <- hash
					break
				}
				elapsed := time.Since(start)
				var s uint64 = uint64(dsize)
				if len(hash) == 64 {
					data, err := ioutil.ReadFile(fpath)
					if err != nil {
						out := fmt.Sprintf("[!] %s => %s failed to download: %s", hash, fpath, err)
						fmt.Println(out)
						results <- hash
						break
					}

					digest := sha256.Sum256(data)
					checksum := hex.EncodeToString(digest[:])
					if hash != checksum {
						out := fmt.Sprintf("[!] %s => %s checksum mismatch!", hash, fpath)
						fmt.Println(out)
						results <- hash
						break

					}
					out := fmt.Sprintf("(%s)(%s) %s => %s\n", elapsed, s, hash[:8], b)
					fmt.Println(out)
					results <- ""
					break

				} else {
					out := fmt.Sprintf("(%s)(%s) %s => %s\n", elapsed, s, hash, b)
					fmt.Println(out)
					results <- ""
					break
				}
			}

		}
	}
}

/*
// Hashlist returns list of snapshots
func Hashlist(bucketname string, configName string, consolebufptr *[]byte, snapshotbuf *[]byte, lock *bool) ([]string, error) {
	log.SetFlags(log.Lshortfile)
	var snapshotlist []string
	*lock = true

	var config Config
	// load config to get ready to upload
	if _, err := toml.DecodeFile(configName, &config); err != nil {
		fmt.Println(err)
		*consolebufptr = []byte(fmt.Sprintln("Error reading config: ", err))
		*lock = false
		return snapshotlist, err
	}
	fmt.Println(config)
	config, err := Readconfig(configName)
	if err != nil {
		fmt.Println(err)
		*consolebufptr = []byte(fmt.Sprintln("Error processing config: ", err))
		*lock = false
		return snapshotlist, err
	}
	// New returns an Amazon S3 compatible client object. API compatibility (v2 or v4) is automatically
	// determined based on the Endpoint value.
	s3Client, err := minio.New(config.Url, config.Accesskey, config.Secretkey, config.Secure)
	if err != nil {
		fmt.Println(err)
		*consolebufptr = []byte(fmt.Sprintln("Error creating S3 client: ", err))
		*lock = false
		return snapshotlist, err
	}
	// Create a done channel to control 'ListObjects' go routine.
	doneCh := make(chan struct{})

	// Indicate to our routine to exit cleanly upon return.
	defer close(doneCh)

	// List all objects from a bucket-name with a matching prefix.
	var snapshots []string
	for object := range s3Client.ListObjects(bucketname, "", config.Secure, doneCh) {
		if object.Err != nil {
			fmt.Println(err)
			*consolebufptr = []byte(fmt.Sprintln(object.Err))
			*lock = false
			return snapshotlist, err
		}
		matched, err := regexp.MatchString(".hsh$", object.Key)
		if err != nil {
			fmt.Println(err)
			*consolebufptr = []byte(fmt.Sprintln(err))
			*lock = false
			return snapshotlist, err

		}
		if matched == true {
			snapshots = append(snapshots, object.Key)
		}
	}
	if len(snapshots) > 0 {
		*snapshotbuf = []byte(snapshots[len(snapshots)-1])
		*lock = false
		*consolebufptr = []byte((fmt.Sprintln("Success: there are ", len(snapshots), " filesystem snapshots available.")))
		return snapshots, nil
	}
	*consolebufptr = []byte(fmt.Sprintln("Error couldn't obtain snapshot list", err))
	*lock = false
	return snapshots, err
}

// Hashseed deploys a hash tree data structure to a directory creating
// downloading all the files and verifying the SHA256 hash
func Hashseed(bucketname string, databasename string, configName string, dir string, consolebufptr *[]byte, curptr *int32, msgbuf *[]byte, nuke bool, lock *bool) {
	log.SetFlags(log.Lshortfile)
	// check for and add trailing / in folder name
	var strs []string
	*lock = true

	slash := dir[(len(dir))-1:]
	if slash != "/" {
		strs = append(strs, dir)
		strs = append(strs, "/")
		dir = strings.Join(strs, "")
	}

	// load config to get ready to upload
	config, err := Readconfig(configName)
	if err != nil {
		*consolebufptr = []byte(fmt.Sprintln("Error unable to load config.", err))
		*lock = false
		return
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
	_, err = downloadfiles.Download(config.Url, config.Port, config.Secure, config.Accesskey, config.Secretkey, config.Enckey, downloadlist, bucketname, consolebufptr, curptr, msgbuf, nuke)
	if err != nil {
		fmt.Println("Error unable to download database:", err)
		*consolebufptr = []byte(fmt.Sprintln("Error unable to download database!", err))
	} else {
		remotedb, err = readdb.Load(strings.Join(dbnameLocal, ""))
		if err != nil {
			*consolebufptr = []byte(fmt.Sprintln("Error unable to read database!", err))
			*lock = false
			return
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
	failedDownloads, err := downloadfiles.Download(config.Url, config.Port, config.Secure, config.Accesskey, config.Secretkey, config.Enckey, dlist, bucketname, consolebufptr, curptr, msgbuf, nuke)
	if err != nil {
		for _, file := range failedDownloads {
			fmt.Println("Error failed to download: ", file)
			*consolebufptr = []byte(fmt.Sprintln(err))
		}
	}
	*consolebufptr = []byte((fmt.Sprintln("Successfully downloaded ", (len(dlist) - len(failedDownloads)), " files. ", len(failedDownloads), " failed.")))
	*lock = false
}
*/

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
	fmt.Println("Files scanned: ", len(dir))

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
	failedDownloads, err := downloadfiles.Download(server, 443, secure, accesskey, secretkey, enckey, downloadlist, bucketname, true)
	if err != nil {
		for _, file := range failedDownloads {
			fmt.Println(err)
			fmt.Println("Error failed to download: ", file)
		}
		fmt.Println(err)
		fmt.Println("Error: Unable to download database. Hash the database been initialised?")
		return false
	}
	// read database
	remotedb, err = readdb.Load(strings.Join(dbnameLocal, ""))
	if err != nil {
		fmt.Println("Unable to read database!", err)
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
	fmt.Println("Verified files: ", c)
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
	failedUploads, err := uploadfiles.Upload(server, 443, secure, accesskey, secretkey, enckey, uploadlist, bucketname)
	if err != nil {
		for _, hash := range failedUploads {
			// remove failed uploads from database
			fmt.Println("Failed to upload: ", hash)

			delete(remotedb, hash)
			delete(hashmapcooked, hash)

		}
		fmt.Println(err)
	}
	fmt.Println("Successfully uploaded ", (len(uploadlist) - len(failedUploads)), " files. ", len(failedUploads), " failed.")
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
		fmt.Println("Error writing to database!", err)
		return false
	}

	// write remotedb to file
	err = writedb.Dump(strings.Join(dbnameLocal, ""), remotedb)
	if err != nil {
		fmt.Println("Error writing to database!", err)
		return false
	}

	dbuploadlist := make(map[string]string)
	// add these files to the upload list
	dbuploadlist[strings.Join(reponame, "")] = strings.Join(hashdb, "")
	dbuploadlist[strings.Join(dbname, "")] = strings.Join(dbnameLocal, "")
	dbuploadlist[strings.Join(dbsnapshot, "")] = strings.Join(dbnameLocal, "")
	failedUploads, err = uploadfiles.Upload(server, 443, secure, accesskey, secretkey, enckey, dbuploadlist, bucketname)
	if err != nil {
		for _, hash := range failedUploads {
			fmt.Println("Failed to upload: ", hash)
		}
		fmt.Println(err)
		err = os.Remove(strings.Join(hashdb, ""))
		err = os.Remove(strings.Join(dbnameLocal, ""))
		if err != nil {
			fmt.Println("Error deleting database!", err)
		}
		return false
	}

	err = os.Remove(strings.Join(hashdb, ""))
	err = os.Remove(strings.Join(dbnameLocal, ""))
	if err != nil {
		fmt.Println("Error deleting database!", err)
		return false
	}
	return true
}

/*
func initRepo() error {
	log.SetFlags(log.Lshortfile)
	if len(os.Args) < 3 {
		usage()
		os.Exit(1)
	}

	// load config to get ready to upload
	usr, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}
	var config Config
	var configName []string
	configName = append(configName, usr.HomeDir)
	configName = append(configName, "/.htcfg")
	config = ReadConfig(strings.Join(configName, ""))
	bucketname := os.Args[2]
	// New returns an Amazon S3 compatible client object. API compatibility (v2 or v4) is automatically
	// determined based on the Endpoint value.
	s3Client, err := minio.New(config.Url, config.Accesskey, config.Secretkey, config.Secure)
	if err != nil {
		log.Fatalln(err)
	}

	found, err := s3Client.BucketExists(bucketname)
	if err != nil {
		return err
	}

	if found {
		fmt.Println("Bucket exists.")
	} else {
		fmt.Println("Creating bucket.")
		err = s3Client.MakeBucket(bucketname, "us-east-1")
		if err != nil {
			log.Fatalln(err)
		}
	}
	var strs []string
	slash := os.Args[3][len(os.Args[3])-1:]
	var dir = os.Args[3]
	if slash != "/" {
		strs = append(strs, os.Args[3])
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
	file, err := os.Create(strings.Join(dbnameLocal, ""))
	defer file.Close()
	if err != nil {
		return err
	}
	dbuploadlist := make(map[string]string)
	// add these files to the upload list
	dbuploadlist[strings.Join(dbname, "")] = strings.Join(dbnameLocal, "")
	err, failedUploads := uploadFiles.Upload(config.Url, config.Port, config.Secure, config.Accesskey, config.Secretkey, config.Enckey, dbuploadlist, bucketname)
	if err != nil {
		for _, hash := range failedUploads {
			fmt.Println("Failed to upload: ", hash)
		}
		return err
	}

	err = os.Remove(strings.Join(dbnameLocal, ""))
	if err != nil {
		fmt.Println("Error deleting database!", err)
	}
	return nil

}*/

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
