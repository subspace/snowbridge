# Relayer flow diagram

## Polkadot to Ethereum
The relayer can be started in 2 different modes for relaying from Polkadot to Ethereum.

## MMR-Epochs-Only mode
This mode runs the relayer in a mode where it ensures that the Ethereum contract receives at least 1 MMR proof per epoch. It only interacts with the Relay Chain Light Client, not the Parachain Light Client.

### Relayer process starts in MMR-Epochs-Only mode
```mermaid
graph TD;
  S1[Query Relay Chain Light Client for latest relayed MMR Epoch];
  S1-->S2;
  S2[Save as latestRelayedEpoch];
  S2-->S3;
  S2-->S5;
  S3[Start querying/watching ethereum for new blocks];
  S3-- New Ethereum Block Found -->S4;
  S4[Add to Ethereum Block Processing Queue];
  S5[Start querying/watching relay chain for new MMR Roots];
  S5-- New Relay Chain MMR Root Found -->S6;
  S6[Add to New MMR Root Processing Queue];
```
### Ethereum Block Processing Queue
```mermaid
graph TD;
  Eb1[New Block In Queue];
  Eb1-- contains updated latestRelayedEpoch -->Eb2
  Eb2[Save as latestRelayedEpoch];
  Eb1-- no update -->Eb3
  Eb3[Ignore];
```
### New MMR Root Processing Queue
```mermaid
graph TD;
  M1[New MMR Root In Queue];
  M1-->M1a;
  M1a[Is epoch == latestRelayedEpoch + 1?];
  M1a-- No -->M2;
  M1a-- Yes -->M3;
  M2[Ignore];
  M3[Is last block of epoch?];
  M3-- Yes -->M4;
  M4[Add new MMR Root to MMR Root Relay queue];
  M3-- No -->M2;
```
### MMR Relay Queue
```mermaid
graph TD;
  Mr00[New MMR to Relay];
  Mr00-->Mr01;
  Mr01[Query relay chain for all signatures on that MMR Root];
  Mr01-->Mr02;
  Mr02[Generate Full Merkle Tree of Public Keys and transaction input data];
  Mr02-->Mr0;
  Mr0[Submit initial verification transaction to relay chain light client];
  Mr0--Transaction Successful -->Mr1;
  Mr1[Watch and wait for x Ethereum blocks until block wait period is over];
  Mr1-->Mr10;
  Mr10[Query blockhash for random seed];
  Mr10-->Mr11;
  Mr11[Generate random number, selected Merkle Proofs and transaction input data];
  Mr11-->Mr2;
  Mr2[Submit final verification transaction to relay chain light client];
  Mr2--Transaction Successful -->Mr3;
  Mr3[Done, Ethereum Block Processing Queue should incidentally receive this new block and update latestRelayedEpoch];
```

## Incentivized-Channel Mode
In this mode, the relayer watches for new incentivized channel commitments, ensuring that everything needed to process that commitment is relayed to Ethereum.
### Relayer process starts in Incentivized-Channel Mode
```mermaid
graph TD;
  S1[Query Parachain Light Client for latest incentivized channel commitment];
  S1-->S2;
  S2[Save as latestIncentivizedChannelCommitment];
  S2-->S3;
  S2-->S5;
  S3[Start querying/watching ethereum for new blocks];
  S3-- New Ethereum Block Found -->S4;
  S4[Add to Ethereum Block Processing Queue];
  S5[Start querying/watching parachain for new incentivized channel commitments];
  S5-- New Incentivized Channel Commitment Found -->S6;
  S6[Add to New Incentivized Channel Commitment Processing Queue];
```
### Ethereum Block Processing Queue
```mermaid
graph TD;
  Eb1[New Block In Queue];
  Eb1-- contains updated latestIncentivizedChannelCommitment -->Eb2
  Eb2[Save as latestIncentivizedChannelCommitment];
  Eb1-- no update -->Eb3
  Eb3[Ignore];
```
### New Incentivized Channel Commitment Processing Queue
```mermaid
graph TD;
  M1[New commitment in queue];
  M1-->M1a;
  M1a[Is it later than latestIncentivizedChannelCommitment?];
  M1a-- No -->M2;
  M1a-- Yes -->M3;
  M2[Ignore];
  M3[Is the relay chain's MMR root far along enough to be able to prove the parachain block for this commitment?];
  M3-- No -->M4;
  M3-- Yes -->M5;
  M4[Wait some time];
  M4-- Retry -->M3;
  M5[Get MMR Root -> MMRLeaf proof for relevant parachain block];
  M6[Get MMRLeaf parachain_heads -> our parachain head merkle proof];
  M7[Query Parachain for all messages in commitment];
  M8[Generate Transaction input data];
  M9[Submit Transaction to Incentivized Channel];
  M5-->M6
  M6-->M7
  M7-->M8
  M8-->M9
  M9--Transaction Successful -->M10;
  M10[Done];
```