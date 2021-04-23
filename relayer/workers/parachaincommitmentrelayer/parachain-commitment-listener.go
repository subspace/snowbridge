// Copyright 2020 Snowfork
// SPDX-License-Identifier: LGPL-3.0-only

package parachaincommitmentrelayer

import (
	"context"
	"errors"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sirupsen/logrus"
	rpcOffchain "github.com/snowfork/go-substrate-rpc-client/v2/rpc/offchain"
	"github.com/snowfork/go-substrate-rpc-client/v2/types"
	"golang.org/x/sync/errgroup"

	"github.com/snowfork/polkadot-ethereum/relayer/chain"
	"github.com/snowfork/polkadot-ethereum/relayer/chain/ethereum"
	"github.com/snowfork/polkadot-ethereum/relayer/chain/parachain"
	"github.com/snowfork/polkadot-ethereum/relayer/chain/relaychain"
	"github.com/snowfork/polkadot-ethereum/relayer/contracts/inbound"
	"github.com/snowfork/polkadot-ethereum/relayer/substrate"
	chainTypes "github.com/snowfork/polkadot-ethereum/relayer/substrate"
)

type ParachainCommitmentListener struct {
	parachainConnection  *parachain.Connection
	relaychainConnection *relaychain.Connection
	ethereumConnection   *ethereum.Connection
	ethereumConfig       *ethereum.Config
	contracts            map[substrate.ChannelID]*inbound.Contract
	messages             chan<- []chain.Message
	log                  *logrus.Entry
}

func NewParachainCommitmentListener(parachainConnection *parachain.Connection,
	relaychainConnection *relaychain.Connection,
	ethereumConnection *ethereum.Connection,
	ethereumConfig *ethereum.Config,
	contracts map[substrate.ChannelID]*inbound.Contract,
	messages chan<- []chain.Message, log *logrus.Entry) *ParachainCommitmentListener {
	return &ParachainCommitmentListener{
		parachainConnection:  parachainConnection,
		relaychainConnection: relaychainConnection,
		ethereumConnection:   ethereumConnection,
		ethereumConfig:       ethereumConfig,
		contracts:            contracts,
		messages:             messages,
		log:                  log,
	}
}

func (li *ParachainCommitmentListener) Start(ctx context.Context, eg *errgroup.Group) error {

	blockNumber, err := li.fetchStartBlock(ctx)
	if err != nil {
		return nil
	}

	headers := make(chan types.Header)

	eg.Go(func() error {
		err = li.produceFinalizedHeaders(ctx, blockNumber, headers)
		close(headers)
		return err
	})

	eg.Go(func() error {
		err := li.consumeFinalizedHeaders(ctx, headers)
		close(li.messages)
		return err
	})

	return nil
}

func sleep(ctx context.Context, delay time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(delay):
	}
}

// Fetch the starting block
func (li *ParachainCommitmentListener) fetchStartBlock(ctx context.Context) (uint64, error) {
	basicContract, err := inbound.NewContract(common.HexToAddress(
		li.ethereumConfig.Channels.Basic.Inbound),
		li.ethereumConnection.GetClient(),
	)
	if err != nil {
		return 0, err
	}

	incentivizedContract, err := inbound.NewContract(common.HexToAddress(
		li.ethereumConfig.Channels.Incentivized.Inbound),
		li.ethereumConnection.GetClient(),
	)
	if err != nil {
		return 0, err
	}

	options := bind.CallOpts{
		Pending: true,
		Context: ctx,
	}

	ethBasicNonce, err := basicContract.Nonce(&options)
	if err != nil {
		return 0, err
	}
	li.log.WithFields(logrus.Fields{
		"nonce": ethBasicNonce,
	}).Info("Checked latest nonce delivered to ethereum basic channel")

	ethIncentivizedNonce, err := incentivizedContract.Nonce(&options)
	if err != nil {
		return 0, err
	}
	li.log.WithFields(logrus.Fields{
		"nonce": ethIncentivizedNonce,
	}).Info("Checked latest nonce delivered to ethereum incentivized channel")

	paraBasicNonceKey, err := types.CreateStorageKey(li.parachainConnection.GetMetadata(), "BasicOutboundModule", "Nonce", nil, nil)
	if err != nil {
		li.log.Error(err)
		panic(err)
	}
	var paraBasicNonce types.U64
	ok, err := li.parachainConnection.GetAPI().RPC.State.GetStorageLatest(paraBasicNonceKey, &paraBasicNonce)
	if err != nil {
		li.log.Error(err)
		panic(err)
	}
	if !ok {
		paraBasicNonce = 0
	}
	li.log.WithFields(logrus.Fields{
		"nonce": uint64(paraBasicNonce),
	}).Info("Checked latest nonce generated by parachain basic channel")

	paraIncentivizedNonceKey, err := types.CreateStorageKey(li.parachainConnection.GetMetadata(), "IncentivizedOutboundModule", "Nonce", nil, nil)
	if err != nil {
		li.log.Error(err)
		panic(err)
	}
	var paraIncentivizedNonce types.U64
	ok, err = li.parachainConnection.GetAPI().RPC.State.GetStorageLatest(paraIncentivizedNonceKey, &paraIncentivizedNonce)
	if err != nil {
		li.log.Error(err)
		panic(err)
	}
	if !ok {
		paraBasicNonce = 0
	}
	li.log.WithFields(logrus.Fields{
		"nonce": uint64(paraIncentivizedNonce),
	}).Info("Checked latest nonce generated by parachain incentivized channel")

	hash, err := li.parachainConnection.GetAPI().RPC.Chain.GetFinalizedHead()
	if err != nil {
		li.log.WithError(err).Error("Failed to fetch hash for starting block")
		return 0, err
	}

	header, err := li.parachainConnection.GetAPI().RPC.Chain.GetHeader(hash)
	if err != nil {
		li.log.WithError(err).Error("Failed to fetch header for starting block")
		return 0, err
	}

	li.log.WithFields(logrus.Fields{
		"blockNumber": header.Number,
	}).Info("Checked latest finalized block number on parachain")

	if ethBasicNonce == uint64(paraBasicNonce) && ethIncentivizedNonce == uint64(paraIncentivizedNonce) {
		return uint64(header.Number), nil
	}

	startingBlockNumber, err := li.searchForCommitment(uint64(header.Number), ethBasicNonce, ethIncentivizedNonce)
	if err != nil {
		return 0, err
	}

	li.log.WithFields(logrus.Fields{
		"blockNumber": startingBlockNumber,
	}).Info("Starting block number found")

	return uint64(startingBlockNumber), nil
}

var ErrBlockNotReady = errors.New("required result to be 32 bytes, but got 0")

func (li *ParachainCommitmentListener) produceFinalizedHeaders(ctx context.Context, startBlock uint64, headers chan<- types.Header) error {
	current := startBlock
	retryInterval := time.Duration(6) * time.Second
	for {
		select {
		case <-ctx.Done():
			li.log.Info("Shutting down producer of finalized headers")
			return ctx.Err()
		default:
			finalizedHash, err := li.parachainConnection.GetAPI().RPC.Chain.GetFinalizedHead()
			if err != nil {
				li.log.WithError(err).Error("Failed to fetch finalized head")
				return err
			}

			finalizedHeader, err := li.parachainConnection.GetAPI().RPC.Chain.GetHeader(finalizedHash)
			if err != nil {
				li.log.WithError(err).Error("Failed to fetch header for finalized head")
				return err
			}

			if current > uint64(finalizedHeader.Number) {
				li.log.WithFields(logrus.Fields{
					"block":  current,
					"latest": finalizedHeader.Number,
				}).Trace("Block is not yet finalized")
				sleep(ctx, retryInterval)
				continue
			}

			hash, err := li.parachainConnection.GetAPI().RPC.Chain.GetBlockHash(current)
			if err != nil {
				if err.Error() == ErrBlockNotReady.Error() {
					sleep(ctx, retryInterval)
					continue
				} else {
					li.log.WithError(err).Error("Failed to fetch block hash")
					return err
				}
			}

			header, err := li.parachainConnection.GetAPI().RPC.Chain.GetHeader(hash)
			if err != nil {
				li.log.WithError(err).Error("Failed to fetch header")
				return err
			}

			headers <- *header
			current = current + 1
		}
	}
}

func (li *ParachainCommitmentListener) consumeFinalizedHeaders(ctx context.Context, headers <-chan types.Header) error {
	if li.messages == nil {
		li.log.Info("Not polling events since channel is nil")
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			li.log.Info("Shutting down consumer of finalized headers")
			return ctx.Err()
		case header, ok := <-headers:
			// check if headers channel has closed
			if !ok {
				return nil
			}
			err := li.processHeader(ctx, header)
			if err != nil {
				return err
			}
		}
	}
}

func (li *ParachainCommitmentListener) processHeader(ctx context.Context, header types.Header) error {

	li.log.WithFields(logrus.Fields{
		"blockNumber": header.Number,
	}).Debug("Processing block")

	digestItem, err := getAuxiliaryDigestItem(header.Digest)
	if err != nil {
		return err
	}

	if digestItem == nil || !digestItem.IsCommitment {
		return nil
	}

	li.log.WithFields(logrus.Fields{
		"block":          header.Number,
		"channelID":      digestItem.AsCommitment.ChannelID,
		"commitmentHash": digestItem.AsCommitment.Hash.Hex(),
	}).Debug("Found commitment hash in header digest")

	messages, err := li.getMessagesForDigestItem(digestItem)
	if err != nil {
		return err
	}

	message := chain.SubstrateOutboundMessage{
		ChannelID:      digestItem.AsCommitment.ChannelID,
		CommitmentHash: digestItem.AsCommitment.Hash,
		Commitment:     messages,
	}

	li.messages <- []chain.Message{message}

	return nil
}

func getAuxiliaryDigestItem(digest types.Digest) (*chainTypes.AuxiliaryDigestItem, error) {
	for _, digestItem := range digest {
		if digestItem.IsOther {
			var auxDigestItem chainTypes.AuxiliaryDigestItem
			err := types.DecodeFromBytes(digestItem.AsOther, &auxDigestItem)
			if err != nil {
				return nil, err
			}
			return &auxDigestItem, nil
		}
	}
	return nil, nil
}

func getParachainHeaderProof(parachainBlockNumber uint64) {

}

func (li *ParachainCommitmentListener) getMessagesForDigestItem(digestItem *chainTypes.AuxiliaryDigestItem) ([]chainTypes.CommitmentMessage, error) {
	storageKey, err := parachain.MakeStorageKey(digestItem.AsCommitment.ChannelID, digestItem.AsCommitment.Hash)
	if err != nil {
		return nil, err
	}

	data, err := li.parachainConnection.GetAPI().RPC.Offchain.LocalStorageGet(rpcOffchain.Persistent, storageKey)
	if err != nil {
		li.log.WithError(err).Error("Failed to read commitment from offchain storage")
		return nil, err
	}

	if data != nil {
		li.log.WithFields(logrus.Fields{
			"commitmentSizeBytes": len(*data),
		}).Debug("Retrieved commitment from offchain storage")
	} else {
		li.log.WithError(err).Error("Commitment not found in offchain storage")
		return nil, err
	}

	var messages []chainTypes.CommitmentMessage

	err = types.DecodeFromBytes(*data, &messages)
	if err != nil {
		li.log.WithError(err).Error("Failed to decode commitment messages")
		return nil, err
	}

	return messages, nil
}

func (li *ParachainCommitmentListener) searchForCommitment(lastBlockNumber uint64, basicNonceToFind uint64, incentivizedNonceToFind uint64) (uint64, error) {
	li.log.WithFields(logrus.Fields{
		"basicNonce":        basicNonceToFind,
		"incentivizedNonce": incentivizedNonceToFind,
		"latestblockNumber": lastBlockNumber,
	}).Debug("Searching backwards from latest block on parachain to find block with nonce")

	basicId := substrate.ChannelID{IsBasic: true}
	incentivizedId := substrate.ChannelID{IsIncentivized: true}

	currentBlockNumber := lastBlockNumber + 1
	basicNonceFound := false
	incentivizedNonceFound := false
	for (basicNonceFound == false || incentivizedNonceFound == false) && currentBlockNumber != 0 {
		currentBlockNumber--
		li.log.WithFields(logrus.Fields{
			"blockNumber": currentBlockNumber,
		}).Debug("Checking header...")

		blockHash, err := li.parachainConnection.GetAPI().RPC.Chain.GetBlockHash(currentBlockNumber)
		if err != nil {
			li.log.WithFields(logrus.Fields{
				"blockNumber": currentBlockNumber,
			}).WithError(err).Error("Failed to fetch blockhash")
			return 0, err
		}

		header, err := li.parachainConnection.GetAPI().RPC.Chain.GetHeader(blockHash)
		if err != nil {
			li.log.WithError(err).Error("Failed to fetch header")
			return 0, err
		}

		digestItem, err := getAuxiliaryDigestItem(header.Digest)
		if err != nil {
			return 0, err
		}

		if digestItem != nil && digestItem.IsCommitment {
			messages, err := li.getMessagesForDigestItem(digestItem)
			if err != nil {
				return 0, err
			}

			for _, message := range messages {
				if (message.Nonce == basicNonceToFind) && (digestItem.AsCommitment.ChannelID == basicId) {
					basicNonceFound = true
				}
				if (message.Nonce == incentivizedNonceToFind) && (digestItem.AsCommitment.ChannelID == incentivizedId) {
					incentivizedNonceFound = true
				}
			}
			if !(basicNonceFound) {
				li.log.WithFields(logrus.Fields{
					"blockNumber": currentBlockNumber,
				}).Error("Basic nonce not found in messages for commitment")
			}
			if !(incentivizedNonceFound) {
				li.log.WithFields(logrus.Fields{
					"blockNumber": currentBlockNumber,
				}).Error("Incentivized nonce not found in messages for commitment")
			}
		}
	}
	return currentBlockNumber, nil
}
