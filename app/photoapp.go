package main

import (
	"fmt"
	"net/http"
	"os"
    //"time"
    "context"
    //"errors"
	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"google.golang.org/appengine"
    "path"
    "io"
)

var (
	// uploadableBucket is the destination bucket.
	// All users will upload files directly to this bucket by using generated Signed URL.
    photoBucketName string
    photoBucket     *storage.BucketHandle
)

func main() {
    photoBucketName = os.Getenv("UPLOADABLE_BUCKET")
    ctx := context.Background()
    client,_ := storage.NewClient(ctx)
	photoBucket = client.Bucket(photoBucketName)

	http.HandleFunc("/uploadPhoto", uploadPhotoHandler)
    appengine.Main()
}
    
func uploadPhoto(r *http.Request) (url string, err error) {
	f, fh, err := r.FormFile("image")

	// random filename, retaining existing extension.
	name := uuid.Must(uuid.New(),err).String() + path.Ext(fh.Filename,)

	ctx := context.Background()
	w := photoBucket.Object(name).NewWriter(ctx)

	// Warning: storage.AllUsers gives public read access to anyone.
	w.ACL = []storage.ACLRule{{Entity: storage.AllUsers, Role: storage.RoleReader}}
	w.ContentType = fh.Header.Get("Content-Type")

	// Entries are immutable, be aggressive about caching (1 day).
	w.CacheControl = "public, max-age=86400"
	const publicURL = "https://storage.googleapis.com/%s/%s"

	if _, err := io.Copy(w, f); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	return fmt.Sprintf(publicURL, photoBucketName, name), nil
}

func uploadPhotoHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Origin", "*")
	publicURL,err := uploadPhoto(r)
    fmt.Fprintf(w,"%s\n%s\n",publicURL,err)
    fmt.Fprintf(w, "Done.\n")
}
