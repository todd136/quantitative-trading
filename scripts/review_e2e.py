#!/usr/bin/env python3
"""Offline independent CLI acceptance; synthetic cases are not strategy returns.
Usage: python3 scripts/review_e2e.py --binary /path/to/backtest --out /tmp/evidence
Requires PyYAML; all market inputs derive from tracked fixture CSVs.
"""
import argparse
import csv
import datetime
import hashlib
import json
import math
import pathlib
import shutil
import subprocess
import yaml

ROOT = pathlib.Path(__file__).resolve().parents[1]

def csv_read(p):
    with p.open(newline='') as f:
        r = csv.DictReader(f)
        return r.fieldnames, list(r)

def csv_write(p, fields, rows):
    with p.open('w', newline='') as f:
        w = csv.DictWriter(f, fieldnames=fields)
        w.writeheader(); w.writerows(rows)

def run(binary, cfg, out):
    out.mkdir(parents=True, exist_ok=True)
    conf = out/'config.yaml'
    conf.write_text(yaml.safe_dump(cfg, sort_keys=False))
    cmd = [str(binary), '-config', str(conf), '-out', str(out)]
    (out/'command.json').write_text(json.dumps(cmd))
    with (out/'stdout.log').open('w') as stdout, (out/'stderr.log').open('w') as stderr:
        r = subprocess.run(cmd, cwd=ROOT, stdout=stdout, stderr=stderr)
    (out/'exitcode').write_text(str(r.returncode)+'\n')
    return r.returncode

def compare(a, b, path='', diffs=None):
    if diffs is None: diffs=[]
    if path == '/generated_at': return diffs
    if isinstance(a, dict) and isinstance(b, dict):
        if a.keys()!=b.keys(): diffs.append([path,'keys differ'])
        for k in a.keys() & b.keys(): compare(a[k],b[k],path+'/'+k,diffs)
    elif isinstance(a,list) and isinstance(b,list):
        if len(a)!=len(b): diffs.append([path,'length differs'])
        for i,(x,y) in enumerate(zip(a,b)): compare(x,y,path+'/'+str(i),diffs)
    elif isinstance(a,(int,float)) and not isinstance(a,bool) and isinstance(b,(int,float)) and not isinstance(b,bool):
        if not math.isclose(a,b,abs_tol=1e-10,rel_tol=1e-9): diffs.append([path,a,b])
    elif a!=b: diffs.append([path,a,b])
    return diffs

def reconcile_audit(dest):
    _, nav = csv_read(dest/'nav.csv')
    _, holdings = csv_read(dest/'holdings_valuation.csv')
    _, claims = csv_read(dest/'corp_action_ledger.csv')
    for n in nav:
        day=n['trade_date']
        hs=[h for h in holdings if h['trade_date']==day]
        cs=[c for c in claims if c['trade_date']==day]
        assert len({h['ts_code'] for h in hs})==len(hs)
        assert len({c['event_id'] for c in cs})==len(cs)
        for h in hs:
            assert h['as_of']<=day
            assert math.isclose(float(h['shares'])*float(h['price']),float(h['market_value']),abs_tol=1e-8)
        mv=sum(float(h['market_value']) for h in hs)
        recv=sum(float(c['receivable_outstanding']) for c in cs)
        assert abs(float(n['cash'])+recv+mv-float(n['nav']))<.00001,(day,n,mv,recv)
        assert abs(recv-float(n['receivable']))<.00001
    return holdings, claims

def main():
    ap=argparse.ArgumentParser();ap.add_argument('--binary',type=pathlib.Path,required=True);ap.add_argument('--out',type=pathlib.Path,required=True)
    args=ap.parse_args();out=args.out.resolve();binary=args.binary.resolve()
    # Refuse stale artifacts that could disguise a failed run.
    if out.exists(): raise SystemExit('output must be a new directory')
    out.mkdir(parents=True)
    fixture=out/'flat_fixture';shutil.copytree(ROOT/'testdata/fixtures',fixture)
    fields, rows=csv_read(fixture/'bars.csv')
    for r in rows:r['adj_factor']='1.0'
    csv_write(fixture/'bars.csv',fields,rows)
    (fixture/'corp_actions.json').write_text(json.dumps(dict(source='independent_synthetic_fixture',provider='fixture',coverage_start='2023-01-01',coverage_end='2025-03-31',complete=True,events=[])))
    cfg=yaml.safe_load((ROOT/'configs/example.yaml').read_text())
    cfg['data'].update(provider='fixture',fixture_dir=str(fixture),strict_pit=True,strict_quotes=True)
    cfg['backtest'].update(start_date='2025-03-01',end_date='2025-03-31')
    cfg['backtest']['sample_split']={'train_end':'2025-03-12','val_end':'2025-03-20'}
    for n in (1,2): assert run(binary,cfg,out/f'repeat{n}')==0
    a=json.loads((out/'repeat1/report.json').read_text());b=json.loads((out/'repeat2/report.json').read_text())
    differences=compare(a,b)
    result={'numeric_abs_tol':1e-10,'numeric_rel_tol':1e-9,'ignored_fields':['/generated_at'],'report_differences':differences}
    for name in ['nav.csv','fills.csv','paper_targets.csv','holdings_valuation.csv','corp_action_ledger.csv']:
        x=out/'repeat1'/name;y=out/'repeat2'/name
        if x.exists() or y.exists(): result[name+'_identical']=x.read_bytes()==y.read_bytes()
    (out/'repeat_comparison.json').write_text(json.dumps(result,indent=2))
    assert not differences, differences[:5]
    assert all(v for k,v in result.items() if k.endswith('_identical'))

    for n in (1,2): reconcile_audit(out/f'repeat{n}')

    # One-name weekly synthetic case with exact cash/share entitlement arithmetic.
    # Buy 1000@50 on 3/4. Record 3/5; ex 3/6; pay 3/7; shares tradable 3/10.
    event_fixture=out/'event_fixture';shutil.copytree(fixture,event_fixture)
    for name in ['securities.csv','bars.csv','financials.csv','market_values.csv','industry.csv','st.csv']:
        fs,rs=csv_read(event_fixture/name)
        rs=[r for r in rs if r.get('ts_code')=='600000.SH']
        if name=='bars.csv':
            for r in rs:
                px=50 if r['trade_date']<'2025-03-06' else (50-.6)/1.4
                for k in ('open','high','low','close','pre_close'):r[k]=str(px)
                r['high_limit']=str(px*1.1);r['low_limit']=str(px*.9);r['adj_factor']='1.0';r['suspended']='0'
        csv_write(event_fixture/name,fs,rs)
    event=dict(ts_code='600000.SH',kind='profit_distribution',announcement_date='2025-03-03',record_date='2025-03-05',ex_date='2025-03-06',cash_pay_date='2025-03-07',shares_tradable_date='2025-03-10',cash_per_share=.6,transfer_ratio=.4)
    meta=json.loads((event_fixture/'corp_actions.json').read_text());meta['events']=[event];(event_fixture/'corp_actions.json').write_text(json.dumps(meta))
    ecfg=json.loads(json.dumps(cfg));ecfg['data']['fixture_dir']=str(event_fixture)
    ecfg['portfolio'].update(N=1,industry_cap_ratio=1,rebalance_freq='weekly',rebalance_weekday=0)
    ecfg['backtest'].update(initial_cash=50000,start_date='2025-03-03',end_date='2025-03-10',sample_split={'train_end':'2025-03-04','val_end':'2025-03-06'})
    ecfg['costs']={'commission_bps':0,'stamp_tax_bps':0,'slippage_bps':0}
    expected=[]
    for d in ['2025-03-03','2025-03-04','2025-03-05','2025-03-06','2025-03-07','2025-03-10']:
        sh=0 if d=='2025-03-03' else 1000 if d<'2025-03-06' else 1400
        cash=50000 if not sh else 0 if d<'2025-03-07' else 600
        expected.append(dict(trade_date=d,cash=cash,shares=sh,receivable=600 if d=='2025-03-06' else 0,nav=50000))
    csv_write(out/'expected_event_ledger.csv',list(expected[0]),expected)
    for n in (1,2):
        dest=out/f'event{n}';assert run(binary,ecfg,dest)==0
        _,actual=csv_read(dest/'nav.csv');assert len(actual)==len(expected)
        for x,e in zip(actual,expected):
            assert x['trade_date']==e['trade_date']
            for k in ['cash','receivable','nav']:assert abs(float(x[k])-e[k])<.01,(k,x,e)
        holdings, claims = reconcile_audit(dest)
        for e in expected:
            hs=[h for h in holdings if h['trade_date']==e['trade_date']]
            assert sum(float(h['shares']) for h in hs)==e['shares']
        for c in claims:
            d=c['trade_date']
            assert d>='2025-03-05'
            assert float(c['shares_at_record'])==1000
            assert float(c['cash_entitled'])==600
            assert float(c['cash_paid_cumulative'])==(600 if d>='2025-03-07' else 0)
            assert float(c['shares_credited_cumulative'])==(400 if d>='2025-03-06' else 0)
            assert c['shares_tradable_date']=='2025-03-10'
        assert len(claims)==4
        _,fills=csv_read(dest/'fills.csv')
        assert len(fills)==1 and fills[0]['side']=='BUY' and float(fills[0]['shares'])==1000 and float(fills[0]['price'])==50
    # Same valid event fixture, remove held-stock quote only: exercise full CLI rejection.
    neg=out/'negative_fixture';shutil.copytree(event_fixture,neg)
    fs,rs=csv_read(neg/'bars.csv');csv_write(neg/'bars.csv',fs,[r for r in rs if r['trade_date']!='2025-03-07'])
    ecfg['data']['fixture_dir']=str(neg)
    assert run(binary,ecfg,out/'negative')!=0
    assert 'missing' in (out/'negative/stderr.log').read_text()
    assert not (out/'negative/report.json').exists()
    # AKShare cache path: a strict provider error must survive the CLI preload loop.
    cache=out/'missing_adj_cache';cache.mkdir()
    def cache_json(name,obj): (cache/(name+'.json')).write_text(json.dumps(obj))
    _,cal=csv_read(event_fixture/'calendar.csv')
    cache_json('calendar',{'dates':[r['trade_date'] for r in cal]})
    cache_json('securities',{'securities':[{'ts_code':'600000.SH','name':'controlled','board':'SSE_MAIN','list_date':'2020-01-01'}]})
    cache_json('st',{'records':[],'source':'controlled'})
    cache_json('industry',{'records':[{'ts_code':'600000.SH','industry_code':'801780','industry_name':'controlled','effective_date':'2020-01-01','source':'SW_L1'}]})
    start=(datetime.date(2025,3,3)-datetime.timedelta(days=400)).isoformat()
    cache_json('bars_600000.SH_'+start+'_2025-03-10',{'bars':[{'ts_code':'600000.SH','trade_date':'2025-03-03','open':50,'close':50,'volume':1000,'adj_factor':1}]})
    acfg=json.loads(json.dumps(ecfg));acfg['data'].update(provider='akshare',akshare_cache_dir=str(cache),akshare_python='/bin/false')
    assert run(binary,acfg,out/'missing_adj_cli')!=0
    message=(out/'missing_adj_cli/stderr.log').read_text()
    assert 'bars preload 600000.SH:' in message and 'adj' in message, message
    assert not (out/'missing_adj_cli/report.json').exists()
    hashes={str(p.relative_to(out)):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(out.rglob('*')) if p.is_file()}
    (out/'manifest.json').write_text(json.dumps(hashes,indent=2))
    (out/'summary.json').write_text(json.dumps({'repeat':'PASS','event_ledger':'PASS','negative_cli':'EXPECTED_REJECTION','missing_adj_cli':'EXPECTED_REJECTION','audit_reconciliation':'PASS','synthetic_only':True},indent=2))
    print('PASS: repeat, independent event ledger, negative CLI; synthetic only')

if __name__=='__main__':main()
