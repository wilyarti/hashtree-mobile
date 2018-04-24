package hashlist

import (
	"fmt"
	"log"
	"os"
	"regexp"

	"github.com/BurntSushi/toml"
	"github.com/minio/minio-go"
)

// Config file struct containing login details
type Config struct {
	Url       string
	Port      int
	Secure    bool
	Accesskey string
	Secretkey string
	Enckey    string
	Directory string
	Bucket    string
}

// Readconfig reads info from config file
func Readconfig(configfile string) (Config, error) {
	var config Config
	_, err := os.Stat(configfile)
	if err != nil {
		fmt.Println("Can't open file: ", configfile, err)
		return config, err
	}
	if _, err := toml.DecodeFile(configfile, &config); err != nil {
		fmt.Println(err)
		return config, err
	}
	return config, nil
}
func Greetings(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}

// Hashlist returns list of snapshots
func Hashlist(bucketname string, configName string) ([]string, error) {
	log.SetFlags(log.Lshortfile)

	var config Config
	var snapshotlist []string
	// load config to get ready to upload
	if _, err := toml.DecodeFile(configName, &config); err != nil {
		fmt.Println(err)
		return snapshotlist, err
	}
	fmt.Println(config)
	config, err := Readconfig(configName)
	if err != nil {
		fmt.Println(err)
		return snapshotlist, err
	}
	// New returns an Amazon S3 compatible client object. API compatibility (v2 or v4) is automatically
	// determined based on the Endpoint value.
	s3Client, err := minio.New(config.Url, config.Accesskey, config.Secretkey, config.Secure)
	if err != nil {
		fmt.Println(err)
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
			return snapshotlist, err
		}
		matched, err := regexp.MatchString(".hsh$", object.Key)
		if err != nil {
			fmt.Println(err)
			return snapshotlist, err

		}
		if matched == true {
			snapshots = append(snapshots, object.Key)
		}
	}
	if len(snapshots) > 0 {
		return snapshots, nil
	}
	return snapshots, err
}
