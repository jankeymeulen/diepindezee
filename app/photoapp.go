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
    "google.golang.org/appengine/datastore"
    "google.golang.org/appengine/image"
    "google.golang.org/appengine/blobstore"
    "encoding/json"
     "google.golang.org/appengine/log"
)

var (
    photoBucketName string
    photoBucket     *storage.BucketHandle
)

type Photo struct {
	Name          string
    PublicURL     string
    ServingURL    string
	Votes         int
}

func main() {
    photoBucketName = os.Getenv("UPLOADABLE_BUCKET")
    ctx := context.Background()
    client,_ := storage.NewClient(ctx)
	photoBucket = client.Bucket(photoBucketName)

	http.HandleFunc("/uploadPhoto", uploadPhotoHandler)
    http.HandleFunc("/listPhoto", listPhotoHandler)
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
	//const publicURL = 

	if _, err := io.Copy(w, f); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	return name, nil
}

func storeDB(r *http.Request,name string) (err error) {
    ctx := appengine.NewContext(r)

    key := datastore.NewIncompleteKey(ctx, "Photo", nil)
    photo := new(Photo)

    photo.Name = name
    photo.PublicURL = fmt.Sprintf("https://storage.googleapis.com/%s/%s",photoBucketName,name)
    blobFilename := fmt.Sprintf("/gs/%s/%s",photoBucketName,name)
    blobkey,_ := blobstore.BlobKeyForFile(ctx,blobFilename)
    var servingURLOptions image.ServingURLOptions
    servingURLOptions.Secure = true
    servingURLOptions.Size = 1200
    servingURLOptions.Crop = false
    servingURL,_ := image.ServingURL(ctx, blobkey,&servingURLOptions)
    photo.ServingURL = servingURL.String()
    photo.Votes = 0

    if _, err := datastore.Put(ctx, key, photo); err != nil {
        return err
    }
    return nil
}

func uploadPhotoHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Origin", "*")
	publicURL,berr := uploadPhoto(r)
    derr := storeDB(r,publicURL)
    fmt.Fprintf(w,"URL %s\nBucketErr %s\nDatastoreErr %s\n",publicURL,berr,derr)
    fmt.Fprintf(w, "Done.\n")
}

func votePhotoHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Origin", "*")

    r.ParseForm()

    ctx := appengine.NewContext(r)
    photos := make([]*Photo, 0)
    q := datastore.NewQuery("Photo").Filter("Name = "+r.Form.name).Order("-Votes")

    keys, _ := q.GetAll(ctx, &photos)

    log.Debugf(ctx, "Found [%d] keys", len(keys))

    jsonPhotos, _ := json.Marshal(photos)
    w.Header().Set("Content-Type", "application/json")
    w.Write(jsonPhotos)
}

func listPhotoHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Origin", "*")

    ctx := appengine.NewContext(r)
    photos := make([]*Photo, 0)
    q := datastore.NewQuery("Photo").
            Order("-Votes")

    keys, _ := q.GetAll(ctx, &photos)

    log.Debugf(ctx, "Found [%d] keys", len(keys))

    jsonPhotos, _ := json.Marshal(photos)
    w.Header().Set("Content-Type", "application/json")
    w.Write(jsonPhotos)
}