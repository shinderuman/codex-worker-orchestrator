package cliinstall

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type cliInstallTransactionJournal struct {
	Version     int                           `json:"version"`
	Destination string                        `json:"destination"`
	State       cliInstallTransactionState    `json:"state"`
	Binaries    []cliInstallTransactionBinary `json:"binaries"`
}

type cliInstallTransactionState struct {
	Path string                          `json:"path"`
	Temp string                          `json:"temp"`
	Pre  cliInstallTransactionFileImage `json:"pre"`
	Post cliInstallTransactionFileImage `json:"post"`
}

type cliInstallTransactionBinary struct {
	Name        string                          `json:"name"`
	Target      string                          `json:"target"`
	Replacement string                          `json:"replacement"`
	Backup      string                          `json:"backup,omitempty"`
	Pre         cliInstallTransactionFileImage `json:"pre"`
	Post        cliInstallTransactionFileImage `json:"post"`
}

type cliInstallTransactionFileImage struct {
	Exists bool   `json:"exists"`
	SHA256 string `json:"sha256,omitempty"`
	Mode   uint32 `json:"mode,omitempty"`
}

type cliInstallTransactionObservation struct {
	statePre  bool
	statePost bool
	binaries  []cliInstallBinaryObservation
}

type cliInstallBinaryObservation struct {
	pre  bool
	post bool
}

const (
	cliInstallTransactionVersion  = 1
	cliInstallTransactionFileName = "cli-install-transaction.json"
)

func applyInstallWithStateWriter(
	actions []action,
	statePath string,
	nextState installState,
	writeState func(string, string) error,
) error {
	changed := changedActions(actions)
	stateDir := filepath.Dir(statePath)
	if err := ensureStateDir(stateDir); err != nil {
		return err
	}
	stateTemp, err := stageState(stateDir, nextState)
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		if err := writeState(stateTemp, statePath); err != nil {
			return errors.Join(fmt.Errorf("commit binary ownership state: %w", err), removeIfExists(stateTemp))
		}
		return nil
	}

	staged, err := stageActions(changed)
	if err != nil {
		return errors.Join(err, removeIfExists(stateTemp))
	}
	journal, err := newCLIInstallTransactionJournal(statePath, stateTemp, staged)
	if err != nil {
		return errors.Join(err, cleanupStaged(staged), removeIfExists(stateTemp))
	}
	if err := saveCLIInstallTransactionJournal(journal); err != nil {
		return errors.Join(err, cleanupStaged(staged), removeIfExists(stateTemp))
	}

	applied, err := commitActions(staged)
	if err != nil {
		return finishFailedCLIInstallTransaction(journal, err)
	}
	if err := writeState(stateTemp, statePath); err != nil {
		cause := errors.Join(fmt.Errorf("commit binary ownership state: %w", err), rollback(applied))
		return finishFailedCLIInstallTransaction(journal, cause)
	}
	return finishCommittedCLIInstallTransaction(journal)
}

func newCLIInstallTransactionJournal(
	statePath string,
	stateTemp string,
	staged []stagedAction,
) (cliInstallTransactionJournal, error) {
	binDir := filepath.Dir(filepath.Dir(statePath))
	destination, err := filepath.Abs(binDir)
	if err != nil {
		return cliInstallTransactionJournal{}, fmt.Errorf("resolve CLI install destination: %w", err)
	}
	state, err := newCLIInstallTransactionState(statePath, stateTemp)
	if err != nil {
		return cliInstallTransactionJournal{}, err
	}
	binaries, err := newCLIInstallTransactionBinaries(staged)
	if err != nil {
		return cliInstallTransactionJournal{}, err
	}
	journal := cliInstallTransactionJournal{
		Version:     cliInstallTransactionVersion,
		Destination: filepath.Clean(destination),
		State:       state,
		Binaries:    binaries,
	}
	if err := validateCLIInstallTransactionJournal(journal, binDir); err != nil {
		return cliInstallTransactionJournal{}, err
	}
	return journal, nil
}

func newCLIInstallTransactionState(statePath, stateTemp string) (cliInstallTransactionState, error) {
	pre, err := snapshotCLIInstallTransactionFile(statePath)
	if err != nil {
		return cliInstallTransactionState{}, err
	}
	post, err := snapshotCLIInstallTransactionFile(stateTemp)
	if err != nil {
		return cliInstallTransactionState{}, err
	}
	return cliInstallTransactionState{
		Path: filepath.Clean(statePath),
		Temp: filepath.Clean(stateTemp),
		Pre:  pre,
		Post: post,
	}, nil
}

func newCLIInstallTransactionBinaries(staged []stagedAction) ([]cliInstallTransactionBinary, error) {
	binaries := make([]cliInstallTransactionBinary, 0, len(staged))
	for _, item := range staged {
		binary, err := newCLIInstallTransactionBinary(item)
		if err != nil {
			return nil, err
		}
		binaries = append(binaries, binary)
	}
	return binaries, nil
}

func newCLIInstallTransactionBinary(item stagedAction) (cliInstallTransactionBinary, error) {
	pre, err := snapshotCLIInstallTransactionFile(item.action.target)
	if err != nil {
		return cliInstallTransactionBinary{}, err
	}
	post, err := snapshotCLIInstallTransactionFile(item.replacement)
	if err != nil {
		return cliInstallTransactionBinary{}, err
	}
	if !post.Exists || post.SHA256 != item.action.sourceHash || post.Mode != 0o755 {
		return cliInstallTransactionBinary{}, fmt.Errorf("staged CLI replacement does not match planned source: %s", item.action.name)
	}
	if item.action.hadTarget != pre.Exists {
		return cliInstallTransactionBinary{}, fmt.Errorf("CLI install preimage changed while staging: %s", item.action.name)
	}
	if err := validateCLIInstallTransactionBackup(item, pre); err != nil {
		return cliInstallTransactionBinary{}, err
	}
	return cliInstallTransactionBinary{
		Name:        item.action.name,
		Target:      filepath.Clean(item.action.target),
		Replacement: filepath.Clean(item.replacement),
		Backup:      cleanOptionalCLIInstallPath(item.backup),
		Pre:         pre,
		Post:        post,
	}, nil
}

func validateCLIInstallTransactionBackup(item stagedAction, pre cliInstallTransactionFileImage) error {
	if !pre.Exists {
		return nil
	}
	backup, err := snapshotCLIInstallTransactionFile(item.backup)
	if err != nil {
		return err
	}
	if !sameCLIInstallTransactionImage(pre, backup) {
		return fmt.Errorf("CLI install backup does not match preimage: %s", item.action.name)
	}
	return nil
}

func saveCLIInstallTransactionJournal(journal cliInstallTransactionJournal) error {
	path := cliInstallTransactionPath(journal.Destination)
	stateDir := filepath.Dir(path)
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return fmt.Errorf("encode CLI install transaction journal: %w", err)
	}
	data = append(data, '\n')
	temp, err := writeTempFile(stateDir, ".cli-install-transaction-*.tmp", bytes.NewReader(data), 0o600)
	if err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		return errors.Join(fmt.Errorf("commit CLI install transaction journal: %w", err), removeIfExists(temp))
	}
	return nil
}

func recoverInterruptedCLIInstall(binDir string) error {
	journal, exists, err := loadCLIInstallTransactionJournal(binDir)
	if err != nil || !exists {
		return err
	}
	observation, err := observeCLIInstallTransaction(journal)
	if err != nil {
		return err
	}
	if err := validateCLIInstallRecoveryEvidence(journal, observation); err != nil {
		return err
	}
	if observation.statePost {
		return finishRecoveredCommittedCLIInstall(journal, observation)
	}
	if !observation.statePre {
		return fmt.Errorf("CLI install transaction ownership state matches neither preimage nor postimage")
	}
	return rollbackRecoveredCLIInstall(journal, observation)
}

func finishRecoveredCommittedCLIInstall(
	journal cliInstallTransactionJournal,
	observation cliInstallTransactionObservation,
) error {
	if err := requireCLIInstallBinaryPostimages(journal, observation); err != nil {
		if observation.statePre {
			return rollbackRecoveredCLIInstall(journal, observation)
		}
		return err
	}
	return cleanupCLIInstallTransaction(journal)
}

func rollbackRecoveredCLIInstall(
	journal cliInstallTransactionJournal,
	observation cliInstallTransactionObservation,
) error {
	if err := restoreCLIInstallTransactionPreimages(journal, observation); err != nil {
		return err
	}
	verified, err := observeCLIInstallTransaction(journal)
	if err != nil {
		return err
	}
	if !verified.statePre {
		return fmt.Errorf("CLI install transaction recovery did not restore ownership state preimage")
	}
	if err := requireCLIInstallBinaryPreimages(journal, verified); err != nil {
		return err
	}
	return cleanupCLIInstallTransaction(journal)
}

func requireCLIInstallBinaryPostimages(
	journal cliInstallTransactionJournal,
	observation cliInstallTransactionObservation,
) error {
	for index, binary := range observation.binaries {
		if !binary.post {
			return fmt.Errorf("CLI install transaction state is committed but binary %s is not the postimage", journal.Binaries[index].Name)
		}
	}
	return nil
}

func requireCLIInstallBinaryPreimages(
	journal cliInstallTransactionJournal,
	observation cliInstallTransactionObservation,
) error {
	for index, binary := range observation.binaries {
		if !binary.pre {
			return fmt.Errorf("CLI install transaction recovery did not restore binary %s preimage", journal.Binaries[index].Name)
		}
	}
	return nil
}

func finishFailedCLIInstallTransaction(journal cliInstallTransactionJournal, cause error) error {
	observation, err := observeCLIInstallTransaction(journal)
	if err != nil {
		return errors.Join(cause, err)
	}
	if !observation.statePre || !allCLIInstallBinariesPre(observation) {
		return errors.Join(cause, fmt.Errorf("CLI install rollback incomplete; durable recovery required"))
	}
	if err := validateCLIInstallRecoveryEvidence(journal, observation); err != nil {
		return errors.Join(cause, err)
	}
	if err := cleanupCLIInstallTransaction(journal); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func finishCommittedCLIInstallTransaction(journal cliInstallTransactionJournal) error {
	observation, err := observeCLIInstallTransaction(journal)
	if err != nil {
		return err
	}
	if !observation.statePost {
		return fmt.Errorf("CLI install transaction commit did not produce ownership state postimage")
	}
	if err := requireCLIInstallBinaryPostimages(journal, observation); err != nil {
		return fmt.Errorf("CLI install transaction commit validation: %w", err)
	}
	if err := validateCLIInstallRecoveryEvidence(journal, observation); err != nil {
		return err
	}
	return cleanupCLIInstallTransaction(journal)
}

func allCLIInstallBinariesPre(observation cliInstallTransactionObservation) bool {
	for _, binary := range observation.binaries {
		if !binary.pre {
			return false
		}
	}
	return true
}

func loadCLIInstallTransactionJournal(binDir string) (cliInstallTransactionJournal, bool, error) {
	path := cliInstallTransactionPath(binDir)
	if err := validateStateDirectory(filepath.Dir(path)); err != nil {
		return cliInstallTransactionJournal{}, false, err
	}
	info, exists, err := lstat(path)
	if err != nil || !exists {
		return cliInstallTransactionJournal{}, exists, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return cliInstallTransactionJournal{}, true, fmt.Errorf("CLI install transaction journal is not a regular file: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return cliInstallTransactionJournal{}, true, fmt.Errorf("open CLI install transaction journal: %w", err)
	}
	journal, decodeErr := decodeCLIInstallTransactionJournal(file)
	closeErr := file.Close()
	if decodeErr != nil {
		return cliInstallTransactionJournal{}, true, errors.Join(decodeErr, closeErr)
	}
	if closeErr != nil {
		return cliInstallTransactionJournal{}, true, fmt.Errorf("close CLI install transaction journal: %w", closeErr)
	}
	if err := validateCLIInstallTransactionJournal(journal, binDir); err != nil {
		return cliInstallTransactionJournal{}, true, err
	}
	return journal, true, nil
}

func decodeCLIInstallTransactionJournal(file *os.File) (cliInstallTransactionJournal, error) {
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var journal cliInstallTransactionJournal
	if err := decoder.Decode(&journal); err != nil {
		return cliInstallTransactionJournal{}, fmt.Errorf("decode CLI install transaction journal: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("trailing data")
		}
		return cliInstallTransactionJournal{}, fmt.Errorf("decode CLI install transaction journal: %w", err)
	}
	return journal, nil
}

func validateCLIInstallTransactionJournal(journal cliInstallTransactionJournal, binDir string) error {
	destination, err := validateCLIInstallTransactionHeader(journal, binDir)
	if err != nil {
		return err
	}
	if err := validateCLIInstallTransactionState(journal.State, destination); err != nil {
		return err
	}
	seen := make(map[string]bool, len(journal.Binaries))
	for _, binary := range journal.Binaries {
		if err := validateCLIInstallTransactionBinary(binary, destination, seen); err != nil {
			return err
		}
		seen[binary.Name] = true
	}
	return nil
}

func validateCLIInstallTransactionHeader(
	journal cliInstallTransactionJournal,
	binDir string,
) (string, error) {
	if journal.Version != cliInstallTransactionVersion {
		return "", fmt.Errorf("invalid CLI install transaction journal version")
	}
	destination, err := filepath.Abs(binDir)
	if err != nil {
		return "", fmt.Errorf("resolve CLI install transaction destination: %w", err)
	}
	destination = filepath.Clean(destination)
	if journal.Destination != destination {
		return "", fmt.Errorf("CLI install transaction destination mismatch")
	}
	return destination, nil
}

func validateCLIInstallTransactionState(state cliInstallTransactionState, destination string) error {
	stateDir := filepath.Join(destination, stateDirName)
	if state.Path != filepath.Join(stateDir, stateFileName) {
		return fmt.Errorf("invalid CLI install transaction state path")
	}
	if !validCLIInstallTempPath(state.Temp, stateDir, ".cli-install-state-") {
		return fmt.Errorf("invalid CLI install transaction state temp path")
	}
	if err := validateCLIInstallTransactionImage(state.Pre); err != nil {
		return fmt.Errorf("invalid CLI install transaction state preimage: %w", err)
	}
	if err := validateCLIInstallTransactionImage(state.Post); err != nil || !state.Post.Exists {
		return fmt.Errorf("invalid CLI install transaction state postimage")
	}
	return nil
}

func validateCLIInstallTransactionBinary(
	binary cliInstallTransactionBinary,
	destination string,
	seen map[string]bool,
) error {
	if _, ok := managedNameSet[binary.Name]; !ok || seen[binary.Name] {
		return fmt.Errorf("invalid CLI install transaction binary name: %s", binary.Name)
	}
	if binary.Target != filepath.Join(destination, binary.Name) {
		return fmt.Errorf("invalid CLI install transaction binary target: %s", binary.Name)
	}
	if !validCLIInstallTempPath(binary.Replacement, destination, "."+binary.Name+"-") {
		return fmt.Errorf("invalid CLI install transaction replacement path: %s", binary.Name)
	}
	if err := validateCLIInstallTransactionBackupPath(binary, destination); err != nil {
		return err
	}
	if err := validateCLIInstallTransactionImage(binary.Pre); err != nil {
		return fmt.Errorf("invalid CLI install transaction binary preimage %s: %w", binary.Name, err)
	}
	if err := validateCLIInstallTransactionImage(binary.Post); err != nil || !binary.Post.Exists || binary.Post.Mode != 0o755 {
		return fmt.Errorf("invalid CLI install transaction binary postimage: %s", binary.Name)
	}
	return nil
}

func validateCLIInstallTransactionBackupPath(binary cliInstallTransactionBinary, destination string) error {
	if binary.Pre.Exists {
		if !validCLIInstallTempPath(binary.Backup, destination, "."+binary.Name+"-backup-") {
			return fmt.Errorf("invalid CLI install transaction backup path: %s", binary.Name)
		}
		return nil
	}
	if binary.Backup != "" {
		return fmt.Errorf("unexpected CLI install transaction backup path: %s", binary.Name)
	}
	return nil
}

func validCLIInstallTempPath(path, parent, prefix string) bool {
	if path == "" || filepath.Dir(path) != filepath.Clean(parent) {
		return false
	}
	base := filepath.Base(path)
	return strings.HasPrefix(base, prefix) && strings.HasSuffix(base, ".tmp")
}

func cleanOptionalCLIInstallPath(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func validateCLIInstallTransactionImage(image cliInstallTransactionFileImage) error {
	if !image.Exists {
		if image.SHA256 != "" || image.Mode != 0 {
			return fmt.Errorf("absent image contains file metadata")
		}
		return nil
	}
	if !validDigest(image.SHA256) {
		return fmt.Errorf("invalid digest")
	}
	if image.Mode == 0 {
		return fmt.Errorf("missing mode")
	}
	return nil
}

func observeCLIInstallTransaction(journal cliInstallTransactionJournal) (cliInstallTransactionObservation, error) {
	statePre, err := cliInstallTransactionFileMatches(journal.State.Path, journal.State.Pre)
	if err != nil {
		return cliInstallTransactionObservation{}, err
	}
	statePost, err := cliInstallTransactionFileMatches(journal.State.Path, journal.State.Post)
	if err != nil {
		return cliInstallTransactionObservation{}, err
	}
	binaries, err := observeCLIInstallTransactionBinaries(journal.Binaries)
	if err != nil {
		return cliInstallTransactionObservation{}, err
	}
	return cliInstallTransactionObservation{statePre: statePre, statePost: statePost, binaries: binaries}, nil
}

func observeCLIInstallTransactionBinaries(
	binaries []cliInstallTransactionBinary,
) ([]cliInstallBinaryObservation, error) {
	observations := make([]cliInstallBinaryObservation, len(binaries))
	for index, binary := range binaries {
		observation, err := observeCLIInstallTransactionBinary(binary)
		if err != nil {
			return nil, err
		}
		observations[index] = observation
	}
	return observations, nil
}

func observeCLIInstallTransactionBinary(binary cliInstallTransactionBinary) (cliInstallBinaryObservation, error) {
	pre, err := cliInstallTransactionFileMatches(binary.Target, binary.Pre)
	if err != nil {
		return cliInstallBinaryObservation{}, err
	}
	post, err := cliInstallTransactionFileMatches(binary.Target, binary.Post)
	if err != nil {
		return cliInstallBinaryObservation{}, err
	}
	if !pre && !post {
		return cliInstallBinaryObservation{}, fmt.Errorf("CLI install transaction binary %s matches neither preimage nor postimage", binary.Name)
	}
	return cliInstallBinaryObservation{pre: pre, post: post}, nil
}

func validateCLIInstallRecoveryEvidence(
	journal cliInstallTransactionJournal,
	observation cliInstallTransactionObservation,
) error {
	if err := validateOptionalCLIInstallEvidence(journal.State.Temp, journal.State.Post); err != nil {
		return fmt.Errorf("invalid CLI install state temp evidence: %w", err)
	}
	for index, binary := range journal.Binaries {
		if err := validateCLIInstallBinaryRecoveryEvidence(binary, observation.binaries[index], observation.statePre); err != nil {
			return err
		}
	}
	return nil
}

func validateCLIInstallBinaryRecoveryEvidence(
	binary cliInstallTransactionBinary,
	observed cliInstallBinaryObservation,
	statePre bool,
) error {
	if err := validateOptionalCLIInstallEvidence(binary.Replacement, binary.Post); err != nil {
		return fmt.Errorf("invalid CLI replacement evidence %s: %w", binary.Name, err)
	}
	if !binary.Pre.Exists {
		return nil
	}
	backupExists, err := validateOptionalCLIInstallEvidenceExists(binary.Backup, binary.Pre)
	if err != nil {
		return fmt.Errorf("invalid CLI backup evidence %s: %w", binary.Name, err)
	}
	if observed.post && statePre && !backupExists {
		return fmt.Errorf("missing CLI backup required to restore %s", binary.Name)
	}
	return nil
}

func validateOptionalCLIInstallEvidence(path string, image cliInstallTransactionFileImage) error {
	_, err := validateOptionalCLIInstallEvidenceExists(path, image)
	return err
}

func validateOptionalCLIInstallEvidenceExists(path string, image cliInstallTransactionFileImage) (bool, error) {
	info, exists, err := lstat(path)
	if err != nil || !exists {
		return exists, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return true, fmt.Errorf("evidence is not a regular file: %s", path)
	}
	matches, err := cliInstallTransactionFileMatches(path, image)
	if err != nil {
		return true, err
	}
	if !matches {
		return true, fmt.Errorf("evidence does not match recorded image: %s", path)
	}
	return true, nil
}

func restoreCLIInstallTransactionPreimages(
	journal cliInstallTransactionJournal,
	observation cliInstallTransactionObservation,
) error {
	for index := len(journal.Binaries) - 1; index >= 0; index-- {
		binary := journal.Binaries[index]
		if !observation.binaries[index].post {
			continue
		}
		if err := restoreCLIInstallTransactionPreimage(binary); err != nil {
			return err
		}
	}
	return nil
}

func restoreCLIInstallTransactionPreimage(binary cliInstallTransactionBinary) error {
	if !binary.Pre.Exists {
		if err := os.Remove(binary.Target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove interrupted CLI install target %s: %w", binary.Target, err)
		}
		return nil
	}
	if err := os.Rename(binary.Backup, binary.Target); err != nil {
		return fmt.Errorf("restore interrupted CLI install target %s: %w", binary.Target, err)
	}
	return nil
}

func cleanupCLIInstallTransaction(journal cliInstallTransactionJournal) error {
	for _, binary := range journal.Binaries {
		if err := cleanupCLIInstallBinaryEvidence(binary); err != nil {
			return err
		}
	}
	if err := removeIfExists(journal.State.Temp); err != nil {
		return fmt.Errorf("remove CLI state temp evidence: %w", err)
	}
	if err := removeIfExists(cliInstallTransactionPath(journal.Destination)); err != nil {
		return fmt.Errorf("remove CLI install transaction journal: %w", err)
	}
	return nil
}

func cleanupCLIInstallBinaryEvidence(binary cliInstallTransactionBinary) error {
	if err := removeIfExists(binary.Replacement); err != nil {
		return fmt.Errorf("remove CLI replacement evidence: %w", err)
	}
	if err := removeIfExists(binary.Backup); err != nil {
		return fmt.Errorf("remove CLI backup evidence: %w", err)
	}
	return nil
}

func snapshotCLIInstallTransactionFile(path string) (cliInstallTransactionFileImage, error) {
	info, exists, err := lstat(path)
	if err != nil || !exists {
		return cliInstallTransactionFileImage{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return cliInstallTransactionFileImage{}, fmt.Errorf("CLI install transaction surface is not a regular file: %s", path)
	}
	digest, err := hashFile(path)
	if err != nil {
		return cliInstallTransactionFileImage{}, err
	}
	return cliInstallTransactionFileImage{Exists: true, SHA256: digest, Mode: uint32(info.Mode().Perm())}, nil
}

func cliInstallTransactionFileMatches(path string, image cliInstallTransactionFileImage) (bool, error) {
	info, exists, err := lstat(path)
	if err != nil {
		return false, err
	}
	if !image.Exists {
		return !exists, nil
	}
	if !exists || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || uint32(info.Mode().Perm()) != image.Mode {
		return false, nil
	}
	digest, err := hashFile(path)
	if err != nil {
		return false, err
	}
	return digest == image.SHA256, nil
}

func sameCLIInstallTransactionImage(left, right cliInstallTransactionFileImage) bool {
	return left.Exists == right.Exists && left.SHA256 == right.SHA256 && left.Mode == right.Mode
}

func cliInstallTransactionPath(binDir string) string {
	return filepath.Join(binDir, stateDirName, cliInstallTransactionFileName)
}
