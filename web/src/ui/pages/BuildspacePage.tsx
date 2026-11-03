import { Gate } from "../shell/Gate";
import BuildspaceView from "../views/BuildspaceView";
export default function BuildspacePage() {
  return <Gate label="Buildspace">{(me) => <BuildspaceView me={me} />}</Gate>;
}
