package merkle

import (
	"fmt"
	"github.com/snowfork/go-substrate-rpc-client/v3/types"
	"math/bits"
)

type SimplifiedMMRProof struct {
	MerkleProofItems   []types.H256
	MerkleProofOrder   uint64
	MMRRightBaggedPeak types.H256
	MMRRestOfThePeaks  []types.H256
	// Below fields are not part of proof directly, but they are included so that
	// we do not lose any information when converting from RPC response
	Blockhash types.H256
	Leaf      types.MMRLeaf
}

func parentOffset(height uint32) uint64 {
	return 2 << height
}

func siblingOffset(height uint32) uint64 {
	return (2 << height) - 1
}

func getPeakPosByHeight(height uint32) uint64 {
	return (1 << (height + 1)) - 2
}

func leftPeakHeightPos(mmrSize uint64) (uint32, uint64) {
	var height uint32 = 1
	var previousPosition uint64 = 0
	pos := getPeakPosByHeight(height)
	for pos < mmrSize {
		height += 1
		previousPosition = pos
		pos = getPeakPosByHeight(height)
	}
	return height - 1, previousPosition
}

func getRightPeak(height uint32, position uint64, mmrSize uint64) (bool, uint32, uint64) {
	position += siblingOffset(height)
	for position > mmrSize-1 {
		if height == 0 {
			return false, 0, 0
		}
		position -= parentOffset(height - 1)
		height -= 1
	}

	return true, height, position
}

func leafIndexToPosition(index uint64) uint64 {
	return leafIndexToMMRSize(index) - uint64(bits.TrailingZeros64(index+1)) - 1
}

func leafCountToMMRSize(leavesCount uint64) uint64 {
	peakCount := uint64(bits.OnesCount64(leavesCount))
	return 2*leavesCount - peakCount
}

func leafIndexToMMRSize(index uint64) uint64 {
	// Leaf index starts from zero
	return leafCountToMMRSize(index + 1)
}

func heightInTree(position uint64) uint32 {
	position += 1
	allOnes := func(num uint64) bool {
		zeroCount := 64 - bits.OnesCount64(num)
		return num != 0 && (bits.LeadingZeros64(num) == zeroCount)
	}
	jumpLeft := func(position uint64) uint64 {
		bitLength := 64 - bits.LeadingZeros64(position)
		mostSignificantBits := 1 << (bitLength - 1)
		return position - uint64(mostSignificantBits-1)
	}

	for !allOnes(position) {
		position = jumpLeft(position)
	}

	return uint32(64 - bits.LeadingZeros64(position) - 1)
}

func getPeaks(mmrSize uint64) []uint64 {
	var peaksPositions []uint64
	var ok bool
	height, position := leftPeakHeightPos(mmrSize)
	peaksPositions = append(peaksPositions, position)
	for height > 0 {
		ok, height, position = getRightPeak(height, position, mmrSize)
		if !ok {
			break
		}
		peaksPositions = append(peaksPositions, position)
	}
	return peaksPositions
}

func CalculateMerkleProofOrder(leavePos uint64, proofItems []types.H256) (error, uint64) {
	var proofOrder uint64
	currentBitFieldPosition := 0

	type QueueElem struct {
		Height   uint32
		Position uint64
	}
	var queue []QueueElem
	queue = append(queue, QueueElem{
		Height:   0,
		Position: leavePos,
	})

	proofItemIterationPosition := 0

	for len(queue) > 0 {
		if proofItemIterationPosition >= len(proofItems) {
			// We have reached an end
			return nil, proofOrder
		}

		var lastElem QueueElem
		lastElem, queue = queue[len(queue)-1], queue[:len(queue)-1]

		nextHeight := heightInTree(lastElem.Position + 1)

		var isSiblingLeft bool
		var siblingElem QueueElem
		if nextHeight > lastElem.Height {
			proofOrder = proofOrder | 1 << currentBitFieldPosition
			isSiblingLeft = true
			siblingElem = QueueElem{
				Height:   lastElem.Height,
				Position: lastElem.Position - siblingOffset(lastElem.Height),
			}
		} else {
			isSiblingLeft = false
			siblingElem = QueueElem{
				Height:   lastElem.Height,
				Position: lastElem.Position + siblingOffset(lastElem.Height),
			}
		}
		currentBitFieldPosition += 1
		proofItemIterationPosition += 1

		var parentElem QueueElem
		if isSiblingLeft {
			parentElem = QueueElem{
				Height:   siblingElem.Height + 1,
				Position: siblingElem.Position + parentOffset(siblingElem.Height),
			}
		} else {
			parentElem = QueueElem{
				Height:   siblingElem.Height + 1,
				Position: siblingElem.Position + 1,
			}
		}
		queue = append(queue, parentElem)
	}

	return fmt.Errorf("corrupted proof"), proofOrder
}

func ConvertToSimplifiedMMRProof(blockhash types.H256, leafIndex uint64, leaf types.MMRLeaf, leafCount uint64, proofItems []types.H256) (SimplifiedMMRProof, error) {
	leafPos := leafIndexToPosition(leafIndex)

	var readyMadePeakHashes []types.H256
	var optionalRightBaggedPeak types.H256 = [32]byte{}
	var merkleProof []types.H256

	var proofItemPosition uint64 = 0
	var merkleRootPeakPosition uint64 = 0

	mmrSize := leafCountToMMRSize(leafCount)
	peaks := getPeaks(mmrSize)

	for i := 0; i < len(peaks); i++ {
		if (i == 0 || leafPos > peaks[i-1]) && leafPos <= peaks[i] {
			merkleRootPeakPosition = uint64(i)
			if i == len(peaks)-1 {
				for i := proofItemPosition; i < uint64(len(proofItems)); i++ {
					merkleProof = append(merkleProof, proofItems[i])
				}
			} else {
				for i := proofItemPosition; i < uint64(len(proofItems)-1); i++ {
					merkleProof = append(merkleProof, proofItems[i])
				}
				optionalRightBaggedPeak = proofItems[len(proofItems)-1]
				break
			}
		} else {
			readyMadePeakHashes = append(readyMadePeakHashes, proofItems[proofItemPosition])
			proofItemPosition += 1
		}
	}

	var localizedMerkleRootPosition uint64
	if merkleRootPeakPosition == 0 {
		localizedMerkleRootPosition = leafPos
	} else {
		localizedMerkleRootPosition = leafPos - peaks[merkleRootPeakPosition-1] - 1
	}

	err, proofOrder := CalculateMerkleProofOrder(localizedMerkleRootPosition, merkleProof)
	if err != nil {
		return SimplifiedMMRProof{}, err
	}

	return SimplifiedMMRProof{
		MerkleProofItems:   merkleProof,
		MerkleProofOrder:   proofOrder,
		MMRRightBaggedPeak: optionalRightBaggedPeak,
		MMRRestOfThePeaks:  readyMadePeakHashes,
		Leaf:               leaf,
		Blockhash: blockhash,
	}, nil
}
