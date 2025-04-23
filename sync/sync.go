package sync

import (
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func getFileName(path string) string {
	return filepath.Base(path)
}

func getRelativePath(srcPath string, filePath string) string {
	basePathAbs, _ := filepath.Abs(srcPath)
	fullPathAbs, _ := filepath.Abs(filePath)

	// Now trim the base path from the full path
	relativePath := strings.TrimPrefix(fullPathAbs, basePathAbs)
	relativePath = strings.TrimPrefix(relativePath, string(filepath.Separator)) // remove leading slash if any

	return relativePath
}

func Sync(localPath string) error {
	config := &ssh.ClientConfig{
		User: "czonx",
		Auth: []ssh.AuthMethod{
			ssh.Password("czonx"), // or use ssh.PublicKeys(...)
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // for testing only
	}

	// Connect to remote
	client, err := ssh.Dial("tcp", "192.168.0.44:22", config)
	if err != nil {
		log.Fatal("Failed to dial: ", err)
	}
	defer client.Close()

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		log.Fatal("Failed to create SFTP client: ", err)
	}
	defer sftpClient.Close()

	// Upload file
	srcFile, _ := os.Open(localPath)
	defer srcFile.Close()

	remotePath := "/home/czonx/test2/"
	rootPath := "/Users/kamilnowak/Documents/Dev/devops/devsync"

	remoteFilePath := remotePath + getRelativePath(rootPath, localPath)
	remoteDir := path.Dir(remoteFilePath) // use "path", not "filepath", for SFTP paths

	err = sftpClient.MkdirAll(remoteDir)
	if err != nil {
		fmt.Println("Failed to create remote directories:", err)
		return err
	}

	dstFile, err := sftpClient.Create(remoteFilePath)
	if err != nil {
		fmt.Println("Failed to create remote file:", err)
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		fmt.Println("File transfer failed: ", err)
		return err
	}

	fmt.Println("File uploaded: ", getFileName(localPath))
	return nil
}
