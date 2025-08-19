import http from 'k6/http';
import { group, check } from 'k6';
import fs from 'k6/experimental/fs';
import csv from 'k6/experimental/csv';

// Payloads file
const payloadsFilePath = __ENV.RPC_PAYLOADS_FILE_PATH;
const payloadsFile = await fs.open(payloadsFilePath);
// Using csv parser which is currently the only K6 module that supports reading a file line by line
// Review https://grafana.com/docs/k6/latest/javascript-api/k6-experimental/ in the future for other options
const payloadsParser = new csv.Parser(payloadsFile);

// Test config file
const configFilePath = __ENV.RPC_CONFIG_FILE_PATH;
const configFile = open(configFilePath);
const config = JSON.parse(configFile);

export const options = config["options"]

export default async function () {
  const {done, value} = await payloadsParser.next();
  if (done) {
    throw new Error("No more payloads found");
  }

  const rpcEndpoint = __ENV.RPC_CLIENT_ENDPOINT;

  const reqName = value[1];
  const payload = JSON.parse(value[2]);
  try {
    const headers = {
      "Content-Type": "application/json",
    };
    const tags = {
      "req_name": reqName,
      "rpc_method": payload["method"],
    }
    if (reqName === "") {
      tags["req_name"] = payload["method"];
    }

    group(reqName, function() {
      const response = http.post(rpcEndpoint, JSON.stringify(payload), {
        headers: headers,
        tags: tags,
      });
      // Checks
      check(response, {
        'status_200': (r) => r.status === 200,
        'has_result': (r) => {
          const data = r.json();
          return data !== undefined && data.result !== undefined && data.error === undefined;
        },
      }, tags);
    });
  } catch (e) {
    console.error(e);
  }
}
