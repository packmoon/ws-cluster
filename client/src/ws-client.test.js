import ws from './ws-client';
import ByteBuffer from 'byte';
import { equal, deepEqual } from 'assert';

it("buffer", () => {
    let buffer = new ws.Buffer()
    buffer.putUint8(1)
    buffer.putUint32(2)
    buffer.putString("hello")
    console.log(buffer.getBytes())

    let buffer2 = new ws.Buffer([1, 2, 0, 0, 0, 5, 0, 0, 0, 104, 101, 108, 108, 111])
    equal(buffer2.getUint8(), 1)
    equal(buffer2.getUint32(), 2)
    equal(buffer2.getString(), "hello")
})

it('message header', () => {
    let header = new ws.MessageHeader(1, ws.MsgTypeConst.Chat, ws.ScopeConst.Client, "2")

    let buf = new ws.Buffer()
    header.encode(buf)
    let wantBytes = new Uint8Array([1, 0, 0, 0, 3, 1, 1, 0, 0, 0, 50])
    deepEqual(buf.getBytes(), wantBytes)
    console.log(wantBytes)

    let header2 = new ws.MessageHeader()
    header2.decode(new ws.Buffer(wantBytes))

    deepEqual(header2, header)

});

it('chat', () => {
    const secret = "xxx123456"
    let wsclient = new ws.WsClient({ url: "ws://localhost:8080", secret })
    wsclient.onOpen = () => {

    }
    wsclient.onMessage = (msg) => {
        console.log(msg)
    }
    wsclient.login("1")

    let wsclient2 = new ws.WsClient({ url: "ws://localhost:8080", secret })
    wsclient2.onOpen = () => {
        wsclient2.sendToClient('1',1,"hello")
    }
    wsclient2.onMessage = (msg) => {
        
    }
    wsclient2.login("2")
})