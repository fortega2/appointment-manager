package prescription

import (
	"bytes"
	"mime/multipart"
	"testing"

	"appointment-manager/internal/storage"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMultipartFile(t *testing.T, filename string, content []byte) (multipart.File, *multipart.FileHeader) {
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

func TestNewServiceValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		repo          *Repository
		storageClient *storage.Client
		expected      error
	}{
		{name: "nil repository", repo: nil, storageClient: &storage.Client{}, expected: ErrNilRepository},
		{name: "nil storage client", repo: &Repository{}, storageClient: nil, expected: ErrNilStorageClient},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, err := NewService(tt.repo, tt.storageClient)

			require.Error(t, err)
			assert.Nil(t, svc)
			assert.ErrorIs(t, err, tt.expected)
		})
	}
}

func TestServiceCreateNilFile(t *testing.T) {
	t.Parallel()

	svc, err := NewService(&Repository{}, &storage.Client{})
	require.NoError(t, err)

	_, header := newMultipartFile(t, "prescription.pdf", []byte("%PDF-1.4 test"))

	p, err := svc.Create(t.Context(), uuid.Must(uuid.NewV7()), 10, nil, header)

	require.Error(t, err)
	assert.Nil(t, p)
	assert.ErrorIs(t, err, ErrNilFile)
}

func TestServiceCreateNilFileHeader(t *testing.T) {
	t.Parallel()

	svc, err := NewService(&Repository{}, &storage.Client{})
	require.NoError(t, err)

	file, _ := newMultipartFile(t, "prescription.pdf", []byte("%PDF-1.4 test"))

	p, err := svc.Create(t.Context(), uuid.Must(uuid.NewV7()), 10, file, nil)

	require.Error(t, err)
	assert.Nil(t, p)
	assert.ErrorIs(t, err, ErrNilFileHeader)
}

func TestServiceCreateUnsupportedFileType(t *testing.T) {
	t.Parallel()

	svc, err := NewService(&Repository{}, &storage.Client{})
	require.NoError(t, err)

	file, header := newMultipartFile(t, "notes.txt", []byte("just a plain text file"))

	p, err := svc.Create(t.Context(), uuid.Must(uuid.NewV7()), 10, file, header)

	require.Error(t, err)
	assert.Nil(t, p)
	assert.ErrorIs(t, err, ErrUnsupportedFileType)
}
