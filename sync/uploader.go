package sync

import (
	"bufio"
	"bytes"
	"devsync/shared"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type Uploader struct {
	sshClient   *ssh.Client
	config      *ssh.ClientConfig
	sftpClient  *sftp.Client
	addr        string
	createdDirs sync.Map
}

func NewUploader(config Config) *Uploader {
	return &Uploader{
		sshClient: nil,
		config: &ssh.ClientConfig{
			User: config.User,
			Auth: []ssh.AuthMethod{
				ssh.Password(config.Password), // or use ssh.PublicKeys(...)
			},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(), // for testing only
		},
		sftpClient: nil,
		addr:       config.Ip + ":" + config.Port,
	}
}

func (u *Uploader) Init() error {
	var err error
	u.config.SetDefaults()
	u.config.Config.Ciphers = []string{
		"aes128-ctr",
		"aes192-ctr",
		"aes256-ctr",
	}

	u.sshClient, err = ssh.Dial("tcp", u.addr, u.config)
	if err != nil {
		return err
	}

	u.sftpClient, err = sftp.NewClient(u.sshClient)
	if err != nil {
		return err
	}

	return nil
}

func (u *Uploader) Close() {
	u.sftpClient.Close()
	u.sshClient.Close()
}

func (u *Uploader) GetRemoteSnapshot(remoteBasePath string) (map[string]shared.FileInfo, error) {
	session, err := u.sshClient.NewSession()
	if err != nil {
		return nil, err
	}
	defer session.Close()
	var output bytes.Buffer
	session.Stdout = &output

	// Run find with format: relative_path size mod_time
	cmd := fmt.Sprintf(`cd %s && find . -type f -printf '%%P %%s %%T@\n'`, remoteBasePath)
	if err := session.Run(cmd); err != nil {
		return nil, err
	}

	snapshot := make(map[string]shared.FileInfo)
	scanner := bufio.NewScanner(&output)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue
		}
		size, _ := strconv.ParseInt(parts[1], 10, 64)
		modTimeFloat, _ := strconv.ParseFloat(parts[2], 64)
		snapshot[parts[0]] = shared.FileInfo{
			Size:    size,
			ModTime: int64(modTimeFloat),
		}
	}

	return snapshot, nil
}

func (u *Uploader) Sync(localBasePath, localFilePath, remoteFolder string) error {
	// Open local file (for regular file copy — skip for symlinks)
	srcFile, err := os.Open(localFilePath)
	if err != nil {
		return fmt.Errorf("failed to open local file %s: %w", localFilePath, err)
	}
	defer srcFile.Close()

	// Use Lstat to avoid following symlinks
	localFileInfo, err := os.Lstat(localFilePath)
	if err != nil {
		return fmt.Errorf("failed to lstat local file %s: %w", localFilePath, err)
	}

	// Build remote path
	remoteFilePath := remoteFolder + getRelativePath(localBasePath, localFilePath)

	// Handle symlinks
	if localFileInfo.Mode()&os.ModeSymlink != 0 {
		linkTarget, err := os.Readlink(localFilePath)
		if err != nil {
			return fmt.Errorf("failed to read symlink %s: %w", localFilePath, err)
		}

		if err := u.sftpClient.Symlink(linkTarget, remoteFilePath); err != nil {
			return fmt.Errorf("failed to create remote symlink %s -> %s: %w", remoteFilePath, linkTarget, err)
		}

		fmt.Printf("Symlink created: %s -> %s\n", remoteFilePath, linkTarget)
		return nil
	}

	// Determine the parent directory (even for files)
	var remoteDir string
	if localFileInfo.IsDir() {
		remoteDir = remoteFilePath
	} else {
		remoteDir = path.Dir(remoteFilePath)
	}

	// Ensure remote directory exists (cache creation)
	if _, ok := u.createdDirs.Load(remoteDir); !ok {
		if err := u.sftpClient.MkdirAll(remoteDir); err != nil {
			return fmt.Errorf("failed to create remote directory %s: %w", remoteDir, err)
		}
		u.createdDirs.Store(remoteDir, struct{}{})
	}

	// Create remote file
	dstFile, err := u.sftpClient.Create(remoteFilePath)
	if err != nil {
		return fmt.Errorf("failed to create remote file %s: %w", remoteFilePath, err)
	}
	defer dstFile.Close()

	// Copy contents
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy data to %s: %w", remoteFilePath, err)
	}

	// Set timestamps
	if err := u.sftpClient.Chtimes(remoteFilePath, localFileInfo.ModTime(), localFileInfo.ModTime()); err != nil {
		return fmt.Errorf("failed to set timestamps on %s: %w", remoteFilePath, err)
	}

	fmt.Printf("Uploaded: %s\n", getFileName(localFilePath))
	return nil
}

func (u *Uploader) Delete(remoteFilePath string) error {
	err := u.sftpClient.Remove(remoteFilePath)
	if err != nil {
		// If error is because file doesn't exist, ignore it
		if os.IsNotExist(err) {
			return nil
		}
		fmt.Printf("Failed to delete remote file %s: %v\n", remoteFilePath, err)
		return err
	}
	fmt.Printf("Deleted remote file: %s\n", remoteFilePath)
	return nil
}
