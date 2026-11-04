// Compact counts are presentation only: IDs, input values, and API numbers stay exact.
export function compactNumber(value) {
 const n=Number(value);if(!Number.isFinite(n))return '—';
 const abs=Math.abs(n);if(abs<1000)return String(n);
 const unit=abs>=1e12?1e12:abs>=1e9?1e9:abs>=1e6?1e6:1000;
 const suffix={1000:'K',1000000:'M',1000000000:'B',1000000000000:'T'}[unit];
 return (n/unit).toFixed(abs>=100000 || Math.round(abs/unit*10)>=1000 ? 0:1)+suffix;
}
