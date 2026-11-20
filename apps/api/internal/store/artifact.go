package store

import "context"

func (s *Store) GetArtifact(ctx context.Context,tenantID,id string)(Artifact,error){
    var a Artifact
    err:=s.pool.QueryRow(ctx,`
      SELECT id::text,artifact_type,storage_uri,content_type,size_bytes,created_at
      FROM artifacts
      WHERE tenant_id=$1 AND id=$2
    `,tenantID,id).Scan(&a.ID,&a.ArtifactType,&a.StorageURI,&a.ContentType,&a.SizeBytes,&a.CreatedAt)
    return a,err
}
