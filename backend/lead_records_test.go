package main

import (
 "encoding/json"
 "testing"
 "time"
)

func TestLeadRecordsValidation(t *testing.T) {
 input:=M{"leadId":"lead","expectedVersion":0,"contacts":[]LeadContact{{ID:"buyer",Name:"采购",Primary:true}},"contracts":[]LeadContract{{ID:"c",Number:"C-001",Amount:10000,Status:"SIGNED",SignedAt:"2026-10-01",DueAt:"2026-11-01"}},"receipts":[]LeadReceipt{{ID:"r",ContractID:"c",Amount:5000,ReceivedAt:"2026-10-08",Reference:"bank-001"}},"externalReminder":true}
 d,e:=decodeLeadUpdate(json.RawMessage(jsonValue(input)));if e!=nil{t.Fatal(e)}
 current:=Lead{ID:"lead",Status:"NEW"};next,changes,_,e:=applyLeadUpdate(current,d,"",time.Now());if e!=nil || len(changes)!=4 || next.Receipts[0].Amount!=5000{t.Fatalf("records %v %v",changes,e)}
 for _,test:=range []struct{name string;change M}{
  {"null",M{"contacts":nil}},
  {"duplicate primary",M{"contacts":[]LeadContact{{ID:"a",Name:"甲",Primary:true},{ID:"b",Name:"乙",Primary:true}}}},
  {"overpaid",M{"receipts":[]LeadReceipt{{ID:"r",ContractID:"c",Amount:10001,ReceivedAt:"2026-10-08",Reference:"bank-001"}}}},
  {"receipt deleted",M{"receipts":[]LeadReceipt{}}},
  {"receipt changed",M{"receipts":[]LeadReceipt{{ID:"r",ContractID:"c",Amount:5001,ReceivedAt:"2026-10-08",Reference:"bank-001"}}}},
  {"contract cancelled",M{"contracts":[]LeadContract{{ID:"c",Number:"C-001",Amount:10000,Status:"CANCELLED"}}}},
  {"fractional cents",M{"receipts":[]M{{"id":"r","contractId":"c","amount":1.1,"receivedAt":"2026-10-08","reference":"bank-001"}}}},
 } {t.Run(test.name,func(t *testing.T){raw:=M{"leadId":"lead","expectedVersion":0};for k,v:=range test.change{raw[k]=v};d,e:=decodeLeadUpdate(json.RawMessage(jsonValue(raw)));if e==nil{_,_,_,e=applyLeadUpdate(next,d,"",time.Now())};if e==nil{t.Fatal("accepted invalid change")}})}
 _,changes,_,e=applyLeadUpdate(next,d,"",time.Now());if e!=nil || len(changes)!=0{t.Fatalf("no-op records: %v %v",changes,e)}
}

func TestLeadExternalDueAndIdentity(t *testing.T) {
 now:=time.Now().UTC();l:=Lead{ID:"l",Status:"CONTACTED",OwnerID:"admin",ReminderAt:now.Add(-time.Minute).Format(time.RFC3339Nano),ExternalReminder:true}
 if !externalLeadDue(l,now){t.Fatal("due reminder ignored")};id:=leadDeliveryID(l);l.Version++;if leadDeliveryID(l)!=id{t.Fatal("notes cause duplicate delivery")}
 l.OwnerID="other";if leadDeliveryID(l)==id{t.Fatal("assignment must change delivery identity")}
 l.ReminderDone=true;if externalLeadDue(l,now){t.Fatal("completed reminder delivered")}
 for _,u:=range []string{"http://gateway.example","https://user:pass@gateway.example","","https://gateway.example/#fragment"}{if leadGatewayURL(u)==nil{t.Fatal("invalid gateway accepted")}}
}
