// SPDX-License-Identifier: Apache-2.0
pragma solidity ^0.8.30;

import {Test, console} from "forge-std/Test.sol";
import {IProofVerifier} from "../src/IProofVerifier.sol";
import {HonkVerifier} from "../src/ultra-honk-verifiers/OuterCount4.sol";

contract OuterCount4LatestTest is Test {
  string internal constant OUTER_RESULT_PATH = "../outputs/latest/outer/outer_result.json";

  function test_VerifyLatestOuterProof() public {
    IProofVerifier verifier = IProofVerifier(address(new HonkVerifier()));
    string memory json = vm.readFile(OUTER_RESULT_PATH);
    string memory proofHex = vm.parseJsonString(json, ".proof");
    string[] memory inputsHex = vm.parseJsonStringArray(json, ".public_inputs");

    bytes memory proof = vm.parseBytes(_normalizeHex(proofHex));
    bytes32[] memory publicInputs = new bytes32[](inputsHex.length);
    for (uint256 i = 0; i < inputsHex.length; i++) {
      publicInputs[i] = vm.parseBytes32(inputsHex[i]);
    }

    console.log("proof bytes");
    console.log(proof.length);
    console.log("public inputs");
    console.log(publicInputs.length);

    assertEq(publicInputs.length, 8, "unexpected public input count");

    (bool success, bytes memory returndata) =
      address(verifier).staticcall(abi.encodeCall(IProofVerifier.verify, (proof, publicInputs)));

    console.log("staticcall success");
    console.log(success);
    console.log("returndata bytes");
    console.log(returndata.length);

    if (success) {
      bool result = abi.decode(returndata, (bool));
      console.log("solidity verifier result");
      console.log(result);
      assertTrue(result, "OuterCount4 verifier returned false");
      return;
    }

    console.logBytes(returndata);
    assertTrue(false, "OuterCount4 verifier reverted");
  }

  function _normalizeHex(string memory raw) internal pure returns (string memory) {
    bytes memory value = bytes(raw);
    if (value.length >= 2 && value[0] == "0" && value[1] == "x") {
      return raw;
    }
    return string.concat("0x", raw);
  }
}
