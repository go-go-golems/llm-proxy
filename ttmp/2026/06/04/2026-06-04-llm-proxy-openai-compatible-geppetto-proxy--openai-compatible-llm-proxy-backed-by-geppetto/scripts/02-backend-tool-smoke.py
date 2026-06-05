#!/usr/bin/env python3
import json, subprocess, sys
from pathlib import Path
PORT=18083
providers=[
 ('openai-chat','openai-chat-smoke','Paris'),
 ('anthropic','anthropic-haiku-smoke','Berlin'),
 ('openai-responses','openai-responses-smoke','Rome'),
]
def req(model,city,stream=False):
 return {
  'model':model,
  'messages':[{'role':'user','content':f'You must call the lookup_weather function exactly once for city {city}. Do not answer directly; return only a tool call.'}],
  'tools':[{'type':'function','function':{'name':'lookup_weather','description':'Look up current weather for a city.','parameters':{'type':'object','properties':{'city':{'type':'string','description':'City name'}},'required':['city'],'additionalProperties':False}}}],
  'tool_choice':'required',
  'stream':stream,
 }
results=[]
for label,model,city in providers:
 p=Path(f'/tmp/backend-tool-{label}.json'); p.write_text(json.dumps(req(model,city),indent=2))
 print(f'\n=== {label} {model} ===', flush=True)
 proc=subprocess.run(['curl','-sS','-w','\nHTTP_STATUS:%{http_code}\n',f'http://127.0.0.1:{PORT}/v1/chat/completions','-H','Content-Type: application/json','--data-binary',f'@{p}'],capture_output=True,text=True,timeout=360)
 raw=proc.stdout; print(raw[:3000])
 body,_,status_part=raw.partition('\nHTTP_STATUS:')
 r={'label':label,'model':model,'city':city,'curl_rc':proc.returncode,'http_status':status_part.strip() if status_part else '?'}
 try:
  d=json.loads(body); c=(d.get('choices') or [{}])[0]; m=c.get('message') or {}
  r.update({'object':d.get('object'),'finish_reason':c.get('finish_reason'),'content':m.get('content'),'tool_calls':m.get('tool_calls'),'error':d.get('error')})
 except Exception as e:
  r.update({'parse_error':str(e),'body_preview':body[:500]})
 results.append(r)
Path('/tmp/backend-tool-smoke-summary.json').write_text(json.dumps(results,indent=2))
print('\n=== SUMMARY ===')
print(json.dumps(results,indent=2))
failed=[]
for r in results:
 calls=r.get('tool_calls') or []
 ok=r.get('http_status')=='200' and r.get('finish_reason')=='tool_calls' and calls and calls[0].get('function',{}).get('name')=='lookup_weather'
 if not ok: failed.append(r['label'])
if failed:
 print('FAILED_LABELS='+','.join(failed), file=sys.stderr)
 sys.exit(1)
