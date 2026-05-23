interface EmptyProps {
  title: string;
  hint?: string;
}

export default function Empty({ title, hint }: EmptyProps) {
  return (
    <div className="card">
      <div className="card-body text-center py-12">
        <p className="text-sm font-medium text-bodh-600">{title}</p>
        {hint && <p className="text-xs text-bodh-400 mt-2">{hint}</p>}
      </div>
    </div>
  );
}
