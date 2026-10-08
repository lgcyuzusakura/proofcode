// Run by run_mechanism_bench.ps1 inside agent-engine to import the actual code.
// These controlled mechanism checks do not measure LLM task quality.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	codecontext "github.com/proofcode-dev/proofcode/agent-engine/internal/context"
	"github.com/proofcode-dev/proofcode/agent-engine/internal/model"
)

type compressionSample struct {
	Case string `json:"case"`
	Copies int `json:"copies"`
	Before int `json:"beforeEstimatedTokens"`
	After int `json:"afterEstimatedTokens"`
	Deduplicated int `json:"deduplicatedMessages"`
	OriginalUnchanged bool `json:"originalUnchanged"`
	LatestExact bool `json:"latestExact"`
	Reduction float64 `json:"estimatedReduction"`
}

func main() {
	if len(os.Args) != 2 { panic("one output path is required") }
	ctx := context.Background()
	log := strings.Repeat("INFO dependency inspection completed\n", 200)
	var samples []compressionSample
	for _, name := range []string{"successful_log", "failed_log", "source_code"} {
		for _, copies := range []int{1,2,4,8} {
			text, isError := log, false
			if name == "failed_log" { text, isError = "error: failed assertion\n"+log, true }
			if name == "source_code" { text = strings.Repeat("func Add(a, b int) int { return a + b }\n",200) }
			body, err := json.Marshal(map[string]any{"content":text,"isError":isError,"metadata":map[string]any{"exitCode":0}})
			must(err)
			messages := []model.Message{{Role:model.RoleSystem,Content:"You are a coding agent."},{Role:model.RoleUser,Content:"Review the current repository."}}
			for i:=0;i<copies;i++ {
				id := fmt.Sprintf("log-%d",i)
				messages=append(messages,model.Message{Role:model.RoleAssistant,ToolCalls:[]model.ToolCall{{ID:id,Name:"run_command",Arguments:json.RawMessage(`{"program":"go","args":["test","./..."]}`)}}},model.Message{Role:model.RoleTool,ToolCallID:id,Content:string(body)})
			}
			before,_:=json.Marshal(messages)
			view,report:=codecontext.CompressMessages(messages)
			after,_:=json.Marshal(messages)
			samples=append(samples,compressionSample{name,copies,report.BeforeEstimatedTokens,report.AfterEstimatedTokens,report.DeduplicatedMessages,string(before)==string(after),view[len(view)-1].Content==string(body),1-float64(report.AfterEstimatedTokens)/float64(report.BeforeEstimatedTokens)})
		}
	}
	root,err:=os.MkdirTemp("","pct-"); must(err); defer os.RemoveAll(root)
	write:=func(name,content string){must(os.WriteFile(filepath.Join(root,name),[]byte(content),0600))}
	write("go.mod","module synthetic\n\ngo 1.24\n")
	write("calc.go","package synthetic\n\nfunc Sum(a, b int) int { return a - b }\n")
	write("calc_test.go","package synthetic\n\nfunc TestSum() { _ = Sum(2, 3) }\n")
	write("other.go","package synthetic\n\nfunc Product(a, b int) int { return a * b }\n")
	git:=func(args ...string){cmd:=exec.CommandContext(ctx,"git",args...);cmd.Dir=root; if output,err:=cmd.CombinedOutput();err!=nil {panic(fmt.Sprintf("git %v: %s: %v",args,output,err))}}
	git("init","-q","-b","main");git("add",".");git("-c","user.name=ThesisFixture","-c","user.email=fixture@example.com","commit","-qm","synthetic baseline")
	retriever:=&codecontext.WorkspaceRetriever{Root:root,ProjectID:"project-a",WorkspaceID:"workspace-a",TaskID:"synthetic-check",AttemptID:"1"}
	first,err:=retriever.Retrieve(ctx,"Sum integer addition", "",8);must(err)
	hot,err:=retriever.Retrieve(ctx,"Sum integer addition", "",8);must(err)
	write("calc.go","package synthetic\n\nfunc Sum(a, b int) int { return a + b }\n")
	changed,err:=retriever.Retrieve(ctx,"Sum integer addition", "",8);must(err)
	changedExact,staleCount:=true,0
	for _,e:=range changed.Evidence { content,err:=os.ReadFile(filepath.Join(root,e.Path));must(err); if e.StartByte<0||e.EndByte>len(content)||string(content[e.StartByte:e.EndByte])!=e.Content {changedExact=false}; if strings.Contains(e.Content,"return a - b"){staleCount++} }
	must(os.Remove(filepath.Join(root,"calc_test.go")))
	deleted,err:=retriever.Retrieve(ctx,"Sum integer addition", "",8);must(err)
	deletedCount:=0;for _,e:=range deleted.Evidence {if e.Path=="calc_test.go"{deletedCount++}}
	retriever.ProjectID="project-b"
	cross,err:=retriever.Retrieve(ctx,"Sum integer addition", "",8);must(err)
	wrongProjectCount:=0;for _,e:=range cross.Evidence {if e.ProjectID!="project-b"{wrongProjectCount++}}
	git("add",".");git("-c","user.name=ThesisFixture","-c","user.email=fixture@example.com","commit","-qm","synthetic changed revision")
	head,err:=retriever.Retrieve(ctx,"Sum integer addition", "",8);must(err)
	result:=map[string]any{
		"kind":"controlled synthetic mechanism verification", "modelQualityClaim":false,"realModelCalls":0,"createdAt":time.Now().UTC().Format(time.RFC3339),
		"compression":samples,
		"retrieval":map[string]any{
			"initial":first,"hot":hot,"changed":changed,"deleted":deleted,"crossProject":cross,"newHead":head,
			"assertions":map[string]bool{"hotQueryCacheHit":hot.CacheHit,"hotEvidenceUnchanged":reflect.DeepEqual(first.Evidence,hot.Evidence),"editChangesSnapshot":first.SnapshotID!=changed.SnapshotID,"changedEvidenceMatchesExactBytes":changedExact,"deletedFileAbsent":deletedCount==0,"crossProjectIsolated":wrongProjectCount==0&&cross.SnapshotID!=deleted.SnapshotID,"headChangesSnapshot":cross.SnapshotID!=head.SnapshotID},
			"staleVersionEvidenceCount":staleCount,"deletedFileEvidenceCount":deletedCount,"wrongProjectEvidenceCount":wrongProjectCount,
		},
	}
	source,err:=os.ReadFile("../../../../thesis/tools/mechanism_bench.go")
	if err==nil { hash:=sha256.Sum256(source);result["harnessSha256"]=hex.EncodeToString(hash[:]) }
	encoded,err:=json.MarshalIndent(result,"","  ");must(err);must(os.WriteFile(os.Args[1],append(encoded,'\n'),0600))
	fmt.Println("controlled mechanism evidence written; no LLM inference")
}

func must(err error){if err!=nil{panic(err)}}
