package sync

import (
	"bufio"
	"bytes"
	"devsync/shared"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type Uploader struct {
	sshClient  *ssh.Client
	config     *ssh.ClientConfig
	sftpClient *sftp.Client
	addr       string
}

func getPrivateKey() (string, error) {
	userHome, err := os.UserHomeDir()
	if err != nil {
		slog.Error("Failed to get current user home directory", "error", err)
		return "", err
	}
	return userHome + "/.ssh/id_rsa", nil
}

func getKnownHosts() (string, error) {
	userHome, err := os.UserHomeDir()
	if err != nil {
		slog.Error("Failed to get current user home directory", "error", err)
		return "", err
	}
	return userHome + "/.ssh/known_hosts", nil
}

func parsePrivateKey(keyPath string) (ssh.Signer, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		slog.Error("Failed to read private key", "error", err)
		return nil, err
	}
	return ssh.ParsePrivateKey(key)
}

func getFileName(path string) string {
	return filepath.Base(path)
}

func NewUploader(config Config) (*Uploader, error) {
	var err error
	keyPath := config.PrivateKey
	if keyPath == "" {
		keyPath, err = getPrivateKey()
		if err != nil {
			return nil, err
		}
	}
	key, err := parsePrivateKey(keyPath)
	if err != nil {
		return nil, err
	}

	knownHosts := config.KnownHosts
	if knownHosts == "" {
		knownHosts, err = getKnownHosts()
		if err != nil {
			return nil, err
		}
	}

	hostKeyCallback, err := knownhosts.New(knownHosts)
	if err != nil {
		return nil, err
	}

	return &Uploader{
		sshClient: nil,
		config: &ssh.ClientConfig{
			User: config.User,
			Auth: []ssh.AuthMethod{
				// ssh.Password(config.Password), // or use ssh.PublicKeys(...)
				// ssh.Password(currentUser), // or use ssh.PublicKeys(...)
				ssh.PublicKeys(key),
			},
			HostKeyCallback: hostKeyCallback,
		},
		sftpClient: nil,
		addr:       config.Ip + ":" + config.Port,
	}, nil
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
	absoluteLocalPath := localBasePath + localFilePath

	// Open local file (for regular file copy — skip for symlinks)
	srcFile, err := os.Open(absoluteLocalPath)
	if err != nil {
		slog.Error("Failed to open local file", "error", err)
		return fmt.Errorf("failed to open local file %s: %w", absoluteLocalPath, err)
	}
	defer srcFile.Close()

	// Use Lstat to avoid following symlinks
	localFileInfo, err := os.Lstat(absoluteLocalPath)
	if err != nil {
		slog.Error("Failed to lstat local file", "error", err)
		return fmt.Errorf("failed to lstat local file %s: %w", absoluteLocalPath, err)
	}

	remoteFilePath := remoteFolder + localFilePath

	// Handle symlinks
	if localFileInfo.Mode()&os.ModeSymlink != 0 {
		linkTarget, err := os.Readlink(absoluteLocalPath)
		if err != nil {
			slog.Error("Failed to read symlink", "error", err, "absoluteLocalPath", absoluteLocalPath)
			return fmt.Errorf("failed to read symlink %s: %w", absoluteLocalPath, err)
		}

		if err := u.sftpClient.Symlink(linkTarget, remoteFilePath); err != nil {
			slog.Error("Failed to create remote symlink", "error", err, "remoteFilePath", remoteFilePath, "linkTarget", linkTarget)
			return fmt.Errorf("failed to create remote symlink %s -> %s: %w", remoteFilePath, linkTarget, err)
		}

		fmt.Printf("Symlink created: %s -> %s\n", remoteFilePath, linkTarget)
		return nil
	}

	var remoteDir string
	if localFileInfo.IsDir() {
		remoteDir = remoteFilePath
	} else {
		remoteDir = path.Dir(remoteFilePath)
	}

	// Ensure remote directory exists
	if err := u.sftpClient.MkdirAll(remoteDir); err != nil {
		slog.Error("Failed to create remote directory", "error", err, "remoteDir", remoteDir)
		return fmt.Errorf("failed to create remote directory %s: %w", remoteDir, err)
	}

	// Create remote file
	dstFile, err := u.sftpClient.Create(remoteFilePath)
	if err != nil {
		slog.Error("failed to create remote file", "error", err, "remoteFilePath", remoteFilePath)
		return fmt.Errorf("failed to create remote file %s: %w", remoteFilePath, err)
	}
	defer dstFile.Close()

	// Copy contents
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		slog.Error("Failed to copy data to remote file", "error", err, "remoteFilePath", remoteFilePath)
		return fmt.Errorf("failed to copy data to %s: %w", remoteFilePath, err)
	}

	// Set timestamps
	if err := u.sftpClient.Chtimes(remoteFilePath, localFileInfo.ModTime(), localFileInfo.ModTime()); err != nil {
		slog.Error("Failed to set timestamps on remote file", "error", err, "remoteFilePath", remoteFilePath)
		return fmt.Errorf("failed to set timestamps on %s: %w", remoteFilePath, err)
	}

	slog.Info("Uploaded", "file", localFilePath)
	return nil
}

func (u *Uploader) Delete(remoteFilePath string) error {
	err := u.sftpClient.Remove(remoteFilePath)
	if err != nil {
		// If error is because file doesn't exist, ignore it
		if os.IsNotExist(err) {
			return nil
		}
		slog.Error("Failed to delete remote file", "error", err, "remoteFilePath", remoteFilePath)
		return fmt.Errorf("failed to delete remote file %s: %w", remoteFilePath, err)
	}
	slog.Info("Deleted remote file", "file", getFileName(remoteFilePath))
	return nil
}
