//go:build integration

package prescription_test

import (
	"appointment-manager/internal/prescription"
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const (
	serviceTestSessions    = 6
	serviceTestPresignTTL  = 5 * time.Minute
	serviceTestFileContent = "%PDF-1.4 fake prescription content"
)

func newServiceTestMultipartFile(t *testing.T, filename string, content []byte) (multipart.File, *multipart.FileHeader) {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("document", filename)
	require.NoError(t, err)
	_, err = fw.Write(content)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	reader := multipart.NewReader(&buf, w.Boundary())
	form, err := reader.ReadForm(int64(len(content)) + 1024)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = form.RemoveAll()
	})

	fileHeader := form.File["document"][0]
	file, err := fileHeader.Open()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = file.Close()
	})

	return file, fileHeader
}

func TestServiceCreateUploadsPersistsAndServesDocument(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newPrescriptionIntegrationPool(ctx, t)
	repo := newPrescriptionIntegrationRepository(t, pool)
	storageClient := newPrescriptionIntegrationStorage(ctx, t)
	patientID := seedPatient(ctx, t, pool)

	svc, err := prescription.NewService(repo, storageClient)
	require.NoError(t, err)

	file, header := newServiceTestMultipartFile(t, "prescription.pdf", []byte(serviceTestFileContent))

	created, err := svc.Create(ctx, patientID, serviceTestSessions, file, header)
	require.NoError(t, err)
	require.NotNil(t, created)

	persisted, err := repo.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.FilePath, persisted.FilePath)
	assert.Equal(t, serviceTestSessions, persisted.TotalSessions)

	url, err := svc.PresignedGetURL(ctx, created.ID, serviceTestPresignTTL)
	require.NoError(t, err)
	require.NotEmpty(t, url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, serviceTestFileContent, string(body))

	require.NoError(t, svc.Cancel(ctx, created.ID))

	cancelled, err := repo.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, prescription.StatusCancelled, cancelled.Status)
}

func TestServiceCreateWithInvalidPatientDoesNotPersist(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pool := newPrescriptionIntegrationPool(ctx, t)
	repo := newPrescriptionIntegrationRepository(t, pool)
	storageClient := newPrescriptionIntegrationStorage(ctx, t)

	svc, err := prescription.NewService(repo, storageClient)
	require.NoError(t, err)

	invalidPatientID := uuid.Must(uuid.NewV7())
	file, header := newServiceTestMultipartFile(t, "prescription.pdf", []byte(serviceTestFileContent))

	created, err := svc.Create(ctx, invalidPatientID, serviceTestSessions, file, header)

	require.Error(t, err)
	assert.Nil(t, created)
	assert.ErrorIs(t, err, prescription.ErrInvalidPatient)
	assert.Equal(t, int64(0), countPrescriptions(ctx, t, pool))
}
