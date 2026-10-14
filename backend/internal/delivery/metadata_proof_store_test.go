package delivery

import (
 "context"
 "strings"
 "github.com/jackc/pgx/v5"
)

type metadataProofPinDB struct {accepted bool;calls int;body string;cancel context.CancelFunc}
type metadataProofPinRow struct {db *metadataProofPinDB}
func(d *metadataProofPinDB)QueryRow(_ context.Context,q string,args ...any)pgx.Row{
 if !strings.Contains(q,"photo.pin_v30_delivery_metadata_proof(")||len(args)!=5{panic("wrong metadata pin boundary")}
 d.calls++;d.body=args[3].(string)
 return metadataProofPinRow{d}
}
func(r metadataProofPinRow)Scan(dest ...any)error{
 *dest[0].(*bool)=r.db.accepted
 if r.db.cancel!=nil{r.db.cancel()}
 return nil
}

type metadataProofReadDB struct {body,pin string;calls int;err error;cancel context.CancelFunc}
type metadataProofReadRow struct {db *metadataProofReadDB}
func(d *metadataProofReadDB)QueryRow(_ context.Context,q string,args ...any)pgx.Row{
 if !strings.Contains(q,"photo.read_v30_delivery_metadata_proof(")||len(args)!=3{panic("wrong metadata read boundary")}
 d.calls++;return metadataProofReadRow{d}
}
func(r metadataProofReadRow)Scan(dest ...any)error{
 if r.db.err!=nil{return r.db.err}
 *dest[0].(*string)=r.db.body;*dest[1].(*string)=r.db.pin
 if r.db.cancel!=nil{r.db.cancel()}
 return nil
}
