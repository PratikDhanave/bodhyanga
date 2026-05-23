interface LoaderProps {
  what?: string;
}

export default function Loader({ what }: LoaderProps) {
  return (
    <div className="card">
      <div className="card-body text-center py-8">
        <div className="inline-block animate-pulse text-sm text-bodh-400">
          Loading{what ? ` ${what}` : ''}…
        </div>
      </div>
    </div>
  );
}

export function ErrorBox({ error, retry }: { error: Error; retry?: () => void }) {
  return (
    <div className="card border-rose-200 bg-rose-50">
      <div className="card-body">
        <p className="text-sm font-medium text-rose-800">Something went wrong.</p>
        <pre className="text-xs text-rose-700 mt-2 whitespace-pre-wrap">{error.message}</pre>
        {retry && (
          <button onClick={retry} className="btn-secondary mt-3">
            Retry
          </button>
        )}
      </div>
    </div>
  );
}
