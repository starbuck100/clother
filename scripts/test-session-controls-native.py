"""Windows integration: native Claude, all four slash controls and real statusline shell.
Uses a local synthetic gateway only; no provider credentials or paid inference.
Run: python scripts/test-session-controls-native.py --binary dist/clother.exe --claude /path/to/claude-real.exe
"""
import argparse,json,os,pathlib,subprocess,tempfile,threading,http.server,time
parser=argparse.ArgumentParser()
parser.add_argument('--binary',required=True)
parser.add_argument('--claude',required=True)
options=parser.parse_args()
binary=pathlib.Path(options.binary).resolve()
claude=pathlib.Path(options.claude).resolve()
calls=[];proofs=[]
class Gateway(http.server.BaseHTTPRequestHandler):
 def log_message(self,*args):pass
 def do_GET(self):
  models=[{'id':'test/'+m,'context_length':1000000,'pricing':{'prompt':'0','completion':'0'},'supported_parameters':['tools'],'architecture':{'input_modalities':['text'],'output_modalities':['text']},'top_provider':{'max_completion_tokens':4096}} for m in ['alpha','beta']]
  self.send_response(200);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(json.dumps({'data':models}).encode())
 def do_POST(self):
  body=json.loads(self.rfile.read(int(self.headers['Content-Length'])));calls.append(body['model'])
  self.send_response(200);self.send_header('Content-Type','text/event-stream' if body.get('stream') else 'application/json');self.end_headers();self.wfile.flush()
  if body.get('stream'):
   self.wfile.write(('data: '+json.dumps({'id':'test','object':'chat.completion.chunk','model':body['model'],'choices':[{'index':0,'delta':{'role':'assistant','content':'OK'},'finish_reason':None}]})+'\n\n').encode());self.wfile.flush()
  time.sleep(.3)
  overlay=next((root/'.clother-overlays').glob('*/settings.json'))
  settings=json.loads(overlay.read_text());statusenv=dict(env,**settings['env']);statusfile=pathlib.Path(statusenv['CLOTHER_ROUTE_STATUS'])
  state=json.loads(statusfile.read_text())
  bash=pathlib.Path(os.environ['ProgramFiles'])/'Git/bin/bash.exe'
  p=subprocess.run([str(bash),'-c',settings['statusLine']['command']],env=statusenv,input=json.dumps({'context_window':{'current_usage':{'input_tokens':1234}}}),capture_output=True,text=True,encoding='utf8',timeout=10)
  proofs.append({'state':{k:state.get(k) for k in ['model','pinned','free_only','reason']},'statusline':p.stdout.strip(),'status_exit':p.returncode,'status_error':p.stderr})
  result={'id':'clother-test','object':'chat.completion','created':1,'model':body['model'],'choices':[{'index':0,'message':{'role':'assistant','content':'OK'},'finish_reason':'stop'}],'usage':{'prompt_tokens':10,'completion_tokens':1,'total_tokens':11}}
  if body.get('stream'):
   result['choices']=[{'index':0,'delta':{},'finish_reason':'stop'}];self.wfile.write(('data: '+json.dumps(result)+'\n\ndata: [DONE]\n\n').encode())
  else:self.wfile.write(json.dumps(result).encode())
server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Gateway);threading.Thread(target=server.serve_forever,daemon=True).start()
with tempfile.TemporaryDirectory(prefix='native-controls-',ignore_cleanup_errors=True) as tmp:
 root=pathlib.Path(tmp)
 for name in ['config','data','cache','claude','home','bin','project']:(root/name).mkdir()
 base=f'http://127.0.0.1:{server.server_port}'
 (root/'config/config.json').write_text(json.dumps({'version':1,'provider_overrides':{'kilo':{'model':'test/alpha','base_url':base},'openrouter':{'base_url':base}}}))
 original=json.dumps({'statusLine':{'type':'command','command':'echo ORIGINAL'}})
 (root/'claude/settings.json').write_text(original)
 env=dict(os.environ,CLOTHER_CONFIG_DIR=str(root/'config'),CLOTHER_DATA_DIR=str(root/'data'),CLOTHER_CACHE_DIR=str(root/'cache'),CLOTHER_BIN=str(root/'bin'),CLAUDE_CONFIG_DIR=str(root/'claude'),CLOTHER_REAL_CLAUDE=str(claude),USERPROFILE=str(root/'home'),HOME=str(root/'home'),CLOTHER_NO_UPDATE_CHECK='1',CLOTHER_SKIP_SELF_UPDATE='1',CLOTHER_FALLBACK_PLANNER='0',CLOTHER_BENCHMARKS='0',CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC='1')
 p=subprocess.run([str(binary),'install','--yes','--no-shim'],env=env,capture_output=True,text=True,encoding='utf8',timeout=20);assert p.returncode==0,p.stderr
 for action in ['next','pin','free','auto']:
  count=len(calls)
  p=subprocess.run([str(root/'bin/clother.exe'),'kilo','--no-banner','--yolo','--print','/clother:'+action,'--max-turns','1','--no-session-persistence','--tools','Bash','--mcp-config','{"mcpServers":{}}','--strict-mcp-config'],cwd=root/'project',env=env,capture_output=True,text=True,encoding='utf8',errors='replace',timeout=50)
  print(action,p.returncode,p.stdout[-500:],p.stderr.strip(),calls)
  assert p.returncode==0 and 'OK' in p.stdout,(p.stdout[-1500:],p.stderr,calls,proofs)
  assert len(calls)==count+1,calls
  proof=proofs[-1];print(json.dumps(proof))
  assert proof['status_exit']==0 and 'context' in proof['statusline'],proof
  if action=='next':assert calls[-1]=='test/beta' and 'test/beta' in proof['statusline'],proof
  if action=='pin':assert proof['state']['pinned'],proof
  if action=='free':assert proof['state']['free_only'],proof
  if action=='auto':assert not proof['state']['pinned'] and not proof['state']['free_only'],proof
 assert (root/'claude/settings.json').read_text()==original,'original settings changed'
 print(json.dumps({'calls':calls,'proofs':proofs,'original_settings_preserved':True},indent=2))
server.shutdown()
