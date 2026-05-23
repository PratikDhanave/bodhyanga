import Page from '../components/Page';
import Empty from '../components/Empty';

export default function RPM() {
  // RPM live stream is a planned slice — current cmd/care does not expose a
  // persistent observation feed. This page wires the endpoint contract so
  // the next slice can fill it in.
  return (
    <Page title="RPM observations" subtitle="Remote patient monitoring stream">
      <Empty
        title="RPM stream endpoint coming next"
        hint="cmd/care evaluates each POST /rpm/observation in-line today; persistent feed + alert log is a planned slice."
      />
    </Page>
  );
}
